package main

import (
	"github.com/xanzy/go-gitlab"
	"log"
	"time"
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

func setGitlabClient() {
	var err error
	gitlabClient, err = gitlab.NewClient(config.GitlabToken, gitlab.WithBaseURL(config.GitlabUrl))
	if err != nil {
		log.Fatal(err)
	}
}

func fetchClosedLastWeekIssues() []*gitlab.Issue {
	lastWeekDay := time.Now().AddDate(0, 0, 7)
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
	log.Fatal("Not implemented")
}
