package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/google/go-github/github"
	"github.com/nlopes/slack"
	"golang.org/x/oauth2"
)

func main() {
	githubAccessToken := os.Getenv("GH_ACCESS_TOKEN")
	if githubAccessToken == "" {
		fmt.Println("Must set github access token")
		os.Exit(1)
	}
	slackAccessToken := os.Getenv("SLACK_ACCESS_TOKEN")
	if slackAccessToken == "" {
		fmt.Println("Must set slack access token")
		os.Exit(1)
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubAccessToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	var commit, issueComment, prComment bool
	opts := github.ListOptions{PerPage: 50}
	for {
		events, resp, err := client.Activity.ListEventsPerformedByUser(ctx, "paddycarver", false, &opts)
		if err != nil {
			panic(err)
		}
		for _, event := range events {
			if event.GetCreatedAt().Before(time.Now().Add(time.Hour * -24)) {
				break
			}
			switch event.GetType() {
			case "IssueCommentEvent":
				issueComment = true
			case "PullRequestReviewEvent":
				prComment = true
			case "PullRequestReviewCommentEvent":
				prComment = true
			case "PushEvent":
				commit = true
			}
		}
		if issueComment && prComment && commit {
			break
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	slackClient := slack.New(slackAccessToken)

	var missing []string
	if !commit {
		missing = append(missing, "commits pushed")
	}
	if !prComment {
		missing = append(missing, "PRs reviewed")
	}
	if !issueComment {
		missing = append(missing, "issues commented on")
	}
	var slackMessage string
	if len(missing) == 1 {
		slackMessage = "No " + missing[0] + " in the last day!"
	} else if len(missing) == 2 {
		slackMessage = "No " + missing[0] + " or " + missing[1] + " in the last day!"
	} else {
		missing[len(missing)-1] = "or " + missing[len(missing)-1]
		slackMessage = "No " + strings.Join(missing, ", ") + " in the last day!"
	}

	issOpt := github.IssueListOptions{
		Filter:    "assigned",
		State:     "open",
		Sort:      "updated",
		Direction: "ascending",
		ListOptions: github.ListOptions{
			PerPage: 50,
		},
	}

	awaitingIssues := map[string]struct {
		Actions     []string
		LastUpdated time.Time
		Created     time.Time
	}{}
	for {
		issues, resp, err := client.Issues.List(context.Background(), true, &issOpt)
		if err != nil {
			panic(err)
		}
		for _, issue := range issues {
			if issue.GetUpdatedAt().Before(time.Now().Add(time.Hour * 24 * 7 * -1)) {
				awaiting := awaitingIssues[issue.GetHTMLURL()]
				awaiting.Actions = append(awaiting.Actions, "assigned to you")
				awaiting.LastUpdated = issue.GetUpdatedAt()
				awaiting.Created = issue.GetCreatedAt()
				awaitingIssues[issue.GetHTMLURL()] = awaiting
			}
		}
		if resp.NextPage == 0 {
			break
		}
		issOpt.ListOptions.Page = resp.NextPage
	}

	searchOpt := github.SearchOptions{
		Sort:  "updated",
		Order: "ascending",
		ListOptions: github.ListOptions{
			PerPage: 50,
		},
	}
	for {
		res, resp, err := client.Search.Issues(context.Background(), "type:pr is:open review-requested:paddycarver", &searchOpt)
		if err != nil {
			panic(err)
		}
		for _, issue := range res.Issues {
			if issue.GetCreatedAt().Before(time.Now().Add(time.Hour * 24 * 3 * -1)) {
				awaiting := awaitingIssues[issue.GetHTMLURL()]
				awaiting.Actions = append(awaiting.Actions, "awaiting your review")
				awaiting.LastUpdated = issue.GetUpdatedAt()
				awaiting.Created = issue.GetCreatedAt()
				awaitingIssues[issue.GetHTMLURL()] = awaiting
			}
		}
		if resp.NextPage == 0 {
			break
		}
		searchOpt.ListOptions.Page = resp.NextPage
	}

	if slackMessage != "" && len(awaitingIssues) > 0 {
		slackMessage += " Also, the following issues and PRs need your attention:\n"
	}
	for issLink, issDetails := range awaitingIssues {
		slackMessage += "• " + issLink + " has been open since " + humanize.Time(issDetails.Created) + " and hasn't been updated since " + humanize.Time(issDetails.LastUpdated) + " and is " + strings.Join(issDetails.Actions, " and ") + "\n"
	}
	if slackMessage != "" {
		_, _, err := slackClient.PostMessage("@paddy", slackMessage, slack.NewPostMessageParameters())
		if err != nil {
			panic(err)
		}
	}
}
