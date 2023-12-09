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
		SMTPUsername:   "email-username",
		SMTPPassword:   "email-password",
		SMTPHost:       "smtp.domain.com",
		SMTPPort:       "584",
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

func fetchClosedLastWeekIssues() []*gitlab.Issue {
	nowTime := time.Now()
	lastWeekDay := nowTime.AddDate(0, 0, -7)
	closedState := "closed"
	searchOpts := &gitlab.ListIssuesOptions{
		Scope:            gitlab.String("assigned_to_me"),
		AssigneeUsername: &config.GitlabUsername,
		UpdatedAfter:     &lastWeekDay, // TODO: should check for a "deployed" or "completed" tag
		State:            &closedState,
	}

	issues, response, err := gitlabClient.Issues.ListIssues(searchOpts)
	if err != nil || response.Status != "200 OK" {
		log.Fatal(err)
	}

	for i := 0; i < len(issues); i++ {
		issue := issues[i]
		if issue.MovedToID != 0 {
			issue = nil
			issues = slices.Delete(issues, i, i)
		}
	}

	return issues
}

func fetchOpenIssuesOnDueDate(dueDate string) []*gitlab.Issue {
	openedState := "opened"
	searchOpts := &gitlab.ListIssuesOptions{
		Scope:            gitlab.String("assigned_to_me"),
		AssigneeUsername: &config.GitlabUsername,
		DueDate:          &dueDate,
		State:            &openedState,
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
	// TODO: remove duplicates

	return issues
}

func fetchProjectNameMap() map[int]string {
	var membership = true
	nowTime := time.Now()
	projects, response, err := gitlabClient.Projects.ListProjects(&gitlab.ListProjectsOptions{
		Membership:        &membership,
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

// TODO: fetch other "doing" issues (specify label in config)
// TODO: fetch "to do" issues (same)

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
		State:    gitlab.String("opened"), // TODO: use in other cases
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
		issueStr := "* " + projectNameMap[group] + ":\r\n"
		for j := 0; j < len(issueGroup); j++ {
			issue := issueGroup[j]
			issueStr += "\t* [" + issue.Title + "](" + issue.WebURL + ")\r\n"
			dueDate := issue.DueDate
			if dueDate != nil {
				issueStr += "\t\t* Due date: " + dueDate.String() + "\r\n"
			}
			mergeRequest := fetchIssueLastMergeRequest(issue)
			if mergeRequest != nil {
				issueStr += "\t\t* Merge request: [" + mergeRequest.Title + "](" + mergeRequest.WebURL + ")\r\n"
			}
		}
		issuesStrs = append(issuesStrs, issueStr)
	}

	return strings.Join(issuesStrs, "")
}

func formatClosedLastWeekIssues() string {
	issues := fetchClosedLastWeekIssues()
	if len(issues) == 0 {
		return ""
	}
	groupedIssues := groupIssuesByProject(issues)
	if len(groupedIssues) == 0 {
		return ""
	}

	title := "Issues closed last week:\r\n\r\n"
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

	title := "Issues to close this week:\r\n\r\n"
	body := formatGroupedIssues(groupedIssues)

	return title + body + "\r\n"
}

func readAndFormatMainDifficulties() string {
	inputReader := bufio.NewReader(os.Stdin)
	fmt.Println("Main difficulties:")
	difficulties := ""
	for {
		difficulty, err := inputReader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		if len(strings.TrimSpace(difficulty)) == 0 {
			break
		}
		difficulties += "\t* " + difficulty
	}
	if len(strings.TrimSpace(difficulties)) == 0 {
		return ""
	}

	return "Main difficulties:\r\n" + difficulties + "\r\n"
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
	flag.Parse()

	err := readConfig(*configPathPtr) // TODO: does not find default filepath, at least on windows
	if err != nil {
		log.Fatal(err)
	}
	setGitlabClient()

	closedLastWeekIssuesStr := formatClosedLastWeekIssues()
	toCloseWeekIssuesStr := formatToCloseThisWeekIssues()
	mainDifficulties := readAndFormatMainDifficulties()
	sendEmail(closedLastWeekIssuesStr + toCloseWeekIssuesStr + mainDifficulties)
}
