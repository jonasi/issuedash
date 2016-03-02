package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"html/template"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"sort"
	"strconv"
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
	var (
		ms = parseIssues(issues)
		sl = []*milestone{}
	)

	for _, name := range milestones {
		if _, ok := ms[name]; ok {
			sl = append(sl, ms[name])
		}
	}

	return tbl.Execute(os.Stdout, map[string]interface{}{
		"milestones": sl,
	})
}

func parseIssues(ghIssues []github.Issue) map[string]*milestone {
	var (
		msMap    = map[string]*milestone{}
		msCmpMap = map[*milestone]map[string]*component{}
	)

	for i := range ghIssues {
		ghIssue := &ghIssues[i]

		if ghIssue.Milestone == nil {
			continue
		}

		ms := msMap[*ghIssue.Milestone.Title]

		if ms == nil {
			ms = &milestone{
				Milestone:  ghIssue.Milestone,
				Components: components{},
			}

			msMap[*ms.Title] = ms
			msCmpMap[ms] = map[string]*component{}
		}

		var (
			cmpName  string
			typeName string
			days     int
		)

		for j := range ghIssue.Labels {
			lbl := ghIssue.Labels[j].String()

			if strings.HasPrefix(lbl, "component: ") {
				cmpName = lbl[11:]
			}

			if strings.HasPrefix(lbl, "type: ") {
				typeName = lbl[6:]
			}

			if strings.HasPrefix(lbl, "estimate: ") {
				var (
					str = strings.TrimSpace(lbl[10:])
					m   = 0
				)

				if strings.HasSuffix(str, "d") {
					m = 1
				} else if strings.HasSuffix(str, "w") {
					m = 5
				}

				if m > 0 {
					num, _ := strconv.Atoi(str[:len(str)-1])

					if num > 0 {
						days += m * num
					}
				}
			}
		}

		ms.Stats.Total++
		if ghIssue.ClosedAt != nil {
			ms.Stats.Closed++
		} else {
			ms.Stats.Days += days
		}

		if cmpName != "" {
			cmp := msCmpMap[ms][cmpName]

			if cmp == nil {
				cmp = &component{
					Name:   cmpName,
					Issues: issues{},
				}

				msCmpMap[ms][cmpName] = cmp
				ms.Components = append(ms.Components, cmp)
			}

			cmp.Issues = append(cmp.Issues, &issue{
				Issue: ghIssue,
				Type:  typeName,
				Days:  days,
			})

			cmp.Stats.Total++

			if ghIssue.ClosedAt != nil {
				cmp.Stats.Closed++
			} else {
				cmp.Stats.Days += days
			}
		}
	}

	for _, ms := range msMap {
		for _, cmp := range ms.Components {
			sort.Sort(cmp.Issues)
		}

		sort.Sort(ms.Components)
	}

	return msMap
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

type milestone struct {
	*github.Milestone
	Components components
	Stats      struct {
		Closed int
		Total  int
		Days   int
	}
}

func (m *milestone) CompletedBadge() string {
	return "https://img.shields.io/badge/completed-" + url.QueryEscape(fmt.Sprintf("%d/%d", m.Stats.Closed, m.Stats.Total)) + "-blue.svg?style=flat-square"
}

func (m *milestone) DaysBadge() string {
	return "https://img.shields.io/badge/remaining-" + url.QueryEscape(fmt.Sprintf("%dd", m.Stats.Days)) + "-green.svg?style=flat-square"
}

type component struct {
	Name   string
	Issues issues
	Stats  struct {
		Closed int
		Total  int
		Days   int
	}
}

func (c *component) CompletedBadge() string {
	return "https://img.shields.io/badge/completed-" + url.QueryEscape(fmt.Sprintf("%d/%d", c.Stats.Closed, c.Stats.Total)) + "-blue.svg?style=flat-square"
}

func (c *component) DaysBadge() string {
	return "https://img.shields.io/badge/remaining-" + url.QueryEscape(fmt.Sprintf("%dd", c.Stats.Days)) + "-green.svg?style=flat-square"
}

type issue struct {
	*github.Issue
	Type string
	Days int
}

type components []*component

func (sl components) Len() int           { return len(sl) }
func (sl components) Swap(i, j int)      { sl[i], sl[j] = sl[j], sl[i] }
func (sl components) Less(i, j int) bool { return sl[i].Name < sl[j].Name }

type issues []*issue

func (sl issues) Len() int      { return len(sl) }
func (sl issues) Swap(i, j int) { sl[i], sl[j] = sl[j], sl[i] }
func (sl issues) Less(i, j int) bool {
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

var tbl = template.Must(template.New("").Parse(`
<table>
	<thead>
	</thead>
	<tbody>{{ range $i, $ms := .milestones }}
		<tr>
			<td colspan="6">
				<h3>{{ $ms.Title }} <img hspace="5" align="right" src="{{ $ms.DaysBadge }}" /> <img hspace="5" align="right" src="{{ $ms.CompletedBadge }}" /></h3>
			</td>
		</tr>{{ range $j, $cmp := $ms.Components }}
		<tr>
			<td colspan="6">
				<h6>{{ $cmp.Name }} <img hspace="5" align="right" src="{{ $cmp.DaysBadge }}" /> <img hspace="5" align="right" src="{{ $cmp.CompletedBadge }}" /></h6>
			</td>
		</tr>{{ range $k, $issue := $cmp.Issues }}
		<tr>
			<td><a href="{{ $issue.HTMLURL }}">#{{ $issue.Number }}</a></td>
			<td><kbd>{{ $issue.Type }}</kbd></td>
			<td>{{ if $issue.Days }}{{ $issue.Days }}d{{ end }}</td>
			<td>{{ $issue.Title }}</td>
			<td width="60">{{ if $issue.Assignee }}<a href="{{ $issue.Assignee.HTMLURL }}"><img valign="middle" height="30" width="30" src="{{ $issue.Assignee.AvatarURL }} " /></a>{{ end }}</td>
			<td>{{ if $issue.ClosedAt }}☑️{{ end }}</td>
		</tr>{{ end }}{{ end }}{{ end }}
	</tbody>
</table>
`))
