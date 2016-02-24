package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
)

var (
	tok        = flag.String("token", "", "")
	repo       = flag.String("repo", "", "")
	milestones = flag.String("milestones", "", "")
	out        = flag.String("write-issues", "", "")
	fromFile   = flag.String("from-file", "", "")
)

func main() {
	if err := run(); err != nil {
		log.Printf("ERROR: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	var (
		cl     = client(*tok)
		ms     = strings.Split(*milestones, ",")
		err    error
		issues []github.Issue
	)

	if *fromFile != "" {
		issues, err = readIssues(*fromFile)
	} else {
		repoParts := strings.SplitN(*repo, "/", 2)
		issues, err = allIssues(cl, repoParts[0], repoParts[1])
	}

	if err != nil {
		return err
	}

	if err = printIssues(issues, ms); err != nil {
		return err
	}

	if *out != "" {
		if err = writeIssues(issues, *out); err != nil {
			return err
		}
	}

	return nil
}

func client(tok string) *github.Client {
	var (
		src    = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})
		httpCl = oauth2.NewClient(oauth2.NoContext, src)
	)

	return github.NewClient(httpCl)
}

func allIssues(client *github.Client, owner, repo string) ([]github.Issue, error) {
	var (
		allIssues []github.Issue
		page      = 1
	)

	for {
		issues, resp, err := client.Issues.ListByRepo(owner, repo, &github.IssueListByRepoOptions{
			State: "all",
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: 1000,
			},
		})

		if err != nil {
			return nil, err
		}

		allIssues = append(allIssues, issues...)
		page = resp.NextPage

		if page == 0 {
			break
		}
	}

	return allIssues, nil
}

func printIssues(issues []github.Issue, milestones []string) error {
	type stats struct {
		open   int
		closed int
	}

	var (
		// milestone => component => issue
		parsed   = map[string]map[string]Issues{}
		msCounts = map[string]*stats{}
		msURLs   = map[string]string{}
	)

	for i := range issues {
		ms := issues[i].Milestone

		if ms == nil {
			continue
		}

		if parsed[*ms.Title] == nil {
			parsed[*ms.Title] = map[string]Issues{}
			msCounts[*ms.Title] = &stats{}
			msURLs[*ms.Title] = *ms.HTMLURL
		}

		if issues[i].ClosedAt == nil {
			msCounts[*ms.Title].open++
		} else {
			msCounts[*ms.Title].closed++
		}

		for j := range issues[i].Labels {
			lbl := issues[i].Labels[j].String()

			if strings.HasPrefix(lbl, "component: ") {
				parsed[*ms.Title][lbl[11:]] = append(parsed[*ms.Title][lbl[11:]], issues[i])
			}
		}
	}

	for _, ms := range milestones {
		if _, ok := parsed[ms]; !ok {
			continue
		}

		fmt.Printf("### %s [%d/%d]\n", ms, msCounts[ms].closed, msCounts[ms].open+msCounts[ms].closed)
		fmt.Printf("[Issues](%s)\n", msURLs[ms])

		cmpKeys := sortedCmpKeys(parsed[ms])

		for _, cmp := range cmpKeys {
			issues := parsed[ms][cmp]
			fmt.Printf("\n**%s**\n", cmp)

			sort.Sort(issues)

			for i := range issues {
				closed := "[ ]"
				if issues[i].ClosedAt != nil {
					closed = "[x]"
				}

				fmt.Printf("- %s [[#%d]](%s) %s", closed, *issues[i].Number, *issues[i].HTMLURL, strings.TrimSpace(*issues[i].Title))

				if issues[i].Assignee != nil {
					fmt.Printf(" <a href=\"%s\"><img valign=\"middle\" height=25 width=25 src=\"%s\" /></a>", *issues[i].Assignee.HTMLURL, *issues[i].Assignee.AvatarURL)
				}

				fmt.Printf("\n")
			}
		}

		fmt.Println("")
	}

	return nil
}

func writeIssues(issues []github.Issue, file string) error {
	js, err := json.Marshal(issues)

	if err != nil {
		return err
	}

	return ioutil.WriteFile(file, js, 0666)
}

func readIssues(file string) ([]github.Issue, error) {
	b, err := ioutil.ReadFile(file)

	if err != nil {
		return nil, err
	}

	var issues []github.Issue
	if err := json.Unmarshal(b, &issues); err != nil {
		return nil, err
	}

	return issues, nil
}

type Issues []github.Issue

func (sl Issues) Len() int      { return len(sl) }
func (sl Issues) Swap(i, j int) { sl[i], sl[j] = sl[j], sl[i] }
func (sl Issues) Less(i, j int) bool {
	var (
		iClosed   = sl[i].ClosedAt != nil
		jClosed   = sl[j].ClosedAt != nil
		iAssigned = sl[i].Assignee != nil
		jAssigned = sl[j].Assignee != nil
	)

	if iClosed == jClosed {
		if iAssigned == jAssigned {
			return *sl[i].Number < *sl[j].Number
		}

		return !iAssigned
	}

	return !iClosed
}

func sortedCmpKeys(mp map[string]Issues) []string {
	var (
		sl = make([]string, len(mp))
		i  = 0
	)

	for k := range mp {
		sl[i] = k
		i++
	}

	sort.Strings(sl)
	return sl
}
