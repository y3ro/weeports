package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	GitlabUrl   string
	GitlabToken string
	GitlabUsername string
}

func getConfigDir() string {
	var homePath string	
	if runtime.GOOS == "windows" {
		homePath = "HOMEPATH"
	} else {
		homePath = "HOME"
	}

	return filepath.Join(os.Getenv(homePath), ".config")
}

func configFileHelp() string {
	helpConfig := Config{
		GitlabUrl:      "https://timetracking.domain.com",
		GitlabToken:    "secret-token",
		GitlabUsername: "username",
	}

	helpBytes, _ := json.MarshalIndent(helpConfig, "", "    ")
	return string(helpBytes)
}

func openDefaultConfigFile() (*os.File, error) {
	var (
		configPath string
		configFile *os.File
		out        []byte
		err        error
	)

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err = cmd.Output()
	if err == nil {
		repoRoot := strings.TrimSpace(string(out))
		configPath = filepath.Join(repoRoot, configFileName)
		configFile, err = os.Open(configPath)
	}

	if err != nil {
		configDir := getConfigDir()
		err = os.MkdirAll(configDir, os.ModePerm)
		if err != nil {
			log.Fatalf("Error mkdir'ing in readConfig: %s\n", err)
		}

		configPath = filepath.Join(configDir, configFileName)
		configFile, err = os.Open(configPath)
	}

	return configFile, err
}

func checkConfigFields(config *Config) error {
	if config.GitlabUrl == "" {
		return errors.New("No GitLab URL specified in the config file")
	}
	if config.GitlabToken == "" {
		return errors.New("No GitLab secret token specified in the config file")
	}
	if config.GitlabUsername == "" {
		return errors.New("No GitLab username specified in the config file")
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
	defer configFile.Close()

	configBytes, err := io.ReadAll(configFile)
	if err != nil {
		err = fmt.Errorf("Error reading config file in readConfig: %w", err)
		return err
	}

	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		err = fmt.Errorf("Error unmarshalling in readConfig: %w", err)
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
	lastWeekDay := time.Now().AddDate(0, 0, -7)
	closedState := "closed"
	searchOpts := &gitlab.ListIssuesOptions{
		AssigneeUsername: &config.GitlabUsername,
		UpdatedAfter: &lastWeekDay, // TODO: should check for a "deployed" or "completed" tag
		State: &closedState,
	}

	issues, response, err := gitlabClient.Issues.ListIssues(searchOpts)
	if err != nil || response.Status != "200 OK" {
		log.Fatal(err)
	}

	return issues
}

func fetchOpenIssuesOnDueDate(dueDate string) []*gitlab.Issue {
	openedState := "opened"
	searchOpts := &gitlab.ListIssuesOptions{
		AssigneeUsername: &config.GitlabUsername,
		DueDate: &dueDate,
		State: &openedState,
	}
	issues, response, err := gitlabClient.Issues.ListIssues(searchOpts)
	if err != nil || response.Status != "200 OK" {
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

// TODO: fetch other "doing" issues
// TODO: fetch "to do" issues

func main() {
	configPathPtr := flag.String("config", "", "Path to the configuration file")
	flag.Parse()

	err := readConfig(*configPathPtr)
	if err != nil {
		log.Fatal(err)
	}
	setGitlabClient()

	// TODO:
	// issues := fetchClosedLastWeekIssues()
	// fmt.Println(formatIssues(issues))
}
