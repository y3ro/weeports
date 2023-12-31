package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/xanzy/go-gitlab"
)

const (
	configFileName = "weeports.json"
)

var (
	config       Config
	gitlabClient *gitlab.Client
)

type Config struct {
	GitlabUrl      string
	GitlabToken    string
	GitlabUsername string
	SMTPUsername   string
	SMTPPassword   string
	SMTPHost       string
	SMTPPort       string
	RecipientEmail string
}

func getConfigDir() string {
	var homePath string
	if runtime.GOOS == "windows" {
		homeDrive := os.Getenv("HOMEDRIVE")
		homePath = homeDrive + os.Getenv("HOMEPATH")
	} else {
		homePath = os.Getenv("HOME")
	}

	return filepath.Join(homePath, ".config")
}

func configFileHelp() string {
	helpConfig := Config{
		GitlabUrl:      "https://git.domain.com",
		GitlabToken:    "gitlab-secret-token",
		GitlabUsername: "gitlab-username",
		SMTPUsername:   "user@domain.com",
		SMTPPassword:   "email-password",
		SMTPHost:       "smtp.domain.com",
		SMTPPort:       "587",
		RecipientEmail: "manager@domain.com",
	}

	helpBytes, _ := json.MarshalIndent(helpConfig, "", "    ")
	return string(helpBytes)
}

func openDefaultConfigFile() (*os.File, error) {
	configDir := getConfigDir()
	err := os.MkdirAll(configDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Error mkdir'ing in readConfig: %s\n", err)
	}

	configPath := filepath.Join(configDir, configFileName)
	configFile, err := os.Open(configPath)

	return configFile, err
}

func checkConfigFields(config *Config) error {
	if config.GitlabUrl == "" {
		return errors.New("no GitLab URL specified in the config file")
	}
	if config.GitlabToken == "" {
		return errors.New("no GitLab secret token specified in the config file")
	}
	if config.GitlabUsername == "" {
		return errors.New("no GitLab username specified in the config file")
	}
	if config.SMTPUsername == "" {
		log.Fatalln("No SMTP username specified in the config file")
	}
	if config.SMTPPassword == "" {
		log.Fatalln("No SMTP password specified in the config file")
	}
	if config.SMTPHost == "" {
		log.Fatalln("No SMTP host specified in the config file")
	}
	if config.SMTPPort == "" {
		log.Fatalln("No SMTP port specified in the config file")
	}
	if config.RecipientEmail == "" {
		log.Fatalln("No recipient email specified in the config file")
	}

	return nil
}

func readConfig(configPath string) error {
	var (
		configFile *os.File
		err        error
	)

	if len(configPath) == 0 {
		configFile, err = openDefaultConfigFile()
	} else {
		configFile, err = os.Open(configPath)
	}

	if err != nil {
		helpMsg := configFileHelp()
		err = fmt.Errorf("%w\n\nExample configuration:\n\n%s", err, helpMsg)
		return err
	}
	defer func(configFile *os.File) {
		err := configFile.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(configFile)

	configBytes, err := io.ReadAll(configFile)
	if err != nil {
		err = fmt.Errorf("error reading config file in readConfig: %w", err)
		return err
	}

	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		err = fmt.Errorf("error unmarshalling in readConfig: %w", err)
		return err
	}

	return checkConfigFields(&config)
}

func setGitlabClient() {
	var err error
	gitlabClient, err = gitlab.NewClient(config.GitlabToken, gitlab.WithBaseURL(config.GitlabUrl))
	if err != nil {
		log.Fatal(err)
	}
}

func fetchClosedLastWeeksIssues(weeks int) []*gitlab.Issue {
	nowTime := time.Now()
	days := weeks * -7
	searchOpts := &gitlab.ListIssuesOptions{
		Scope:            gitlab.String("assigned_to_me"),
		AssigneeUsername: &config.GitlabUsername,
		UpdatedAfter:     gitlab.Time(nowTime.AddDate(0, 0, days)),
		State:            gitlab.String("closed"),
	}

	issues, response, err := gitlabClient.Issues.ListIssues(searchOpts)
	if err != nil || response.Status != "200 OK" {
		log.Fatal(err)
	}

	for i := 0; i < len(issues); i++ {
		issue := issues[i]
		if issue.MovedToID != 0 {
			issue = nil
			issues = slices.Delete(issues, i, i+1)
		}
	}

	return issues
}

func fetchOpenIssuesOnDueDate(dueDate string) []*gitlab.Issue {
	searchOpts := &gitlab.ListIssuesOptions{
		Scope:            gitlab.String("assigned_to_me"),
		AssigneeUsername: &config.GitlabUsername,
		DueDate:          &dueDate,
		State:            gitlab.String("opened"),
	}
	issues, response, err := gitlabClient.Issues.ListIssues(searchOpts)
	if err != nil || response.StatusCode != 200 {
		log.Fatal(err)
	}

	return issues
}

func fetchToCloseThisWeekIssues() []*gitlab.Issue {
	var issues []*gitlab.Issue
	issues = append(issues, fetchOpenIssuesOnDueDate("week")...)
	issues = append(issues, fetchOpenIssuesOnDueDate("overdue")...)

	return issues
}

func fetchProjectNameMap() map[int]string {
	nowTime := time.Now()
	projects, response, err := gitlabClient.Projects.ListProjects(&gitlab.ListProjectsOptions{
		Membership:        gitlab.Bool(true),
		LastActivityAfter: gitlab.Time(nowTime.AddDate(0, 0, -7)),
	})
	if err != nil || response.StatusCode != 200 {
		log.Fatal(err)
	}

	projectNameMap := make(map[int]string)
	for i := 0; i < len(projects); i++ {
		project := projects[i]
		projectNameMap[project.ID] = project.Name
	}

	return projectNameMap
}

func groupIssuesByProject(issues []*gitlab.Issue) map[int][]*gitlab.Issue {
	projectIssues := make(map[int][]*gitlab.Issue)
	for i := 0; i < len(issues); i++ {
		issue := issues[i]
		projectIssues[issue.ProjectID] = append(projectIssues[issue.ProjectID], issue)
	}

	return projectIssues
}

func slugify(inputString string) string {
	// Compile the regular expression to match non-alphanumeric characters
	regex := regexp.MustCompile("[^a-zA-Z0-9]")

	// Use the regular expression to replace non-alphanumeric characters with an empty string
	cleanedString := regex.ReplaceAllString(inputString, "")

	return strings.ToLower(cleanedString)
}

func fetchIssueLastMergeRequest(issue *gitlab.Issue) *gitlab.MergeRequest {
	listMergeRequestOptions := &gitlab.ListMergeRequestsOptions{
		AuthorID: &issue.Assignee.ID,
		State:    gitlab.String("opened"),
	}
	mergeRequests, response, err := gitlabClient.MergeRequests.ListMergeRequests(listMergeRequestOptions)
	if err != nil || response.StatusCode != 200 {
		log.Fatal(err)
	}

	issueTitleCleaned := slugify(issue.Title)
	for i := 0; i < len(mergeRequests); i++ {
		mergeRequest := mergeRequests[i]
		sourceBranchCleaned := slugify(mergeRequest.SourceBranch)
		if sourceBranchCleaned != issueTitleCleaned {
			mergeRequest = nil
			mergeRequests = slices.Delete(mergeRequests, i, i+1)
		}
	}

	if len(mergeRequests) == 0 {
		return nil
	}
	return mergeRequests[len(mergeRequests)-1]
}

func formatGroupedIssues(groupedIssues map[int][]*gitlab.Issue) string {
	var issuesStrs []string
	projectNameMap := fetchProjectNameMap()
	for group, issueGroup := range groupedIssues {
		if len(issueGroup) == 0 {
			continue
		}
		issueStr := "#### " + projectNameMap[group] + ":\r\n"
		for j := 0; j < len(issueGroup); j++ {
			issue := issueGroup[j]
			issueStr += "  * [" + issue.Title + "](" + issue.WebURL + ")\r\n"
			dueDate := issue.DueDate
			if dueDate != nil {
				issueStr += "    * Due date: " + dueDate.String() + "\r\n"
			}
			mergeRequest := fetchIssueLastMergeRequest(issue)
			if mergeRequest != nil {
				issueStr += "    * Merge request: [" + mergeRequest.Title + "](" + mergeRequest.WebURL + ")\r\n"
			}
		}
		issuesStrs = append(issuesStrs, issueStr)
	}

	return strings.Join(issuesStrs, "")
}

func formatClosedLastWeeksIssues(weeks int) string {
	issues := fetchClosedLastWeeksIssues(weeks)
	if len(issues) == 0 {
		return ""
	}
	groupedIssues := groupIssuesByProject(issues)
	if len(groupedIssues) == 0 {
		return ""
	}

	weeksStr := "last week"
	if weeks > 1 {
		weeksStr = fmt.Sprintf("in the last %d weeks", weeks)
	}
	title := "### Issues closed " + weeksStr + ":\r\n\r\n"
	body := formatGroupedIssues(groupedIssues)

	return title + body + "\r\n"
}

func formatToCloseThisWeekIssues() string {
	issues := fetchToCloseThisWeekIssues()
	if len(issues) == 0 {
		return ""
	}
	groupedIssues := groupIssuesByProject(issues)
	if len(groupedIssues) == 0 {
		return ""
	}

	title := "### Issues to close this week:\r\n\r\n"
	body := formatGroupedIssues(groupedIssues)

	return title + body + "\r\n"
}

func readAndFormatMainDifficulties() string {
	inputReader := bufio.NewReader(os.Stdin)
	mainDifficultiesStr := "### Main difficulties:"
	fmt.Println(mainDifficultiesStr)
	difficulties := ""
	for {
		difficulty, err := inputReader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		if len(strings.TrimSpace(difficulty)) == 0 {
			break
		}
		difficulties += "  * " + difficulty
	}
	if len(strings.TrimSpace(difficulties)) == 0 {
		return ""
	}

	return mainDifficultiesStr + "\r\n" + difficulties + "\r\n"
}

func sendEmail(msgBody string) {
	host := config.SMTPHost
	toStr := config.RecipientEmail
	to := []string{toStr}
	nowTime := time.Now()
	nowString := nowTime.Format("2006-01-02")
	message := []byte("To: " + toStr + "\r\n" +
		"Subject: Weekly report (" + nowString + ")\r\n" +
		"\r\n" + msgBody + "\r\n")

	auth := smtp.PlainAuth("", config.SMTPUsername, config.SMTPPassword, host)
	err := smtp.SendMail(host+":"+config.SMTPPort, auth, config.SMTPUsername, to, message)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Email sent: " + string(message))
}

func main() {
	configPathPtr := flag.String("config", "", "Path to the configuration file")
	weeksPtr := flag.Int("weeks", 1, "Number of weeks to report")
	flag.Parse()

	err := readConfig(*configPathPtr)
	if err != nil {
		log.Fatal(err)
	}
	setGitlabClient()

	closedLastWeeksIssuesStr := formatClosedLastWeeksIssues(*weeksPtr)
	toCloseWeekIssuesStr := formatToCloseThisWeekIssues()
	mainDifficulties := readAndFormatMainDifficulties()
	sendEmail(closedLastWeeksIssuesStr + toCloseWeekIssuesStr + mainDifficulties)
}
