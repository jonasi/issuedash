package main

import (
	"encoding/json"
	"flag"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"html/template"
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

		ms.Stats.Total++
		if ghIssue.ClosedAt != nil {
			ms.Stats.Closed++
		}

		var (
			cmpName  string
			typeName string
		)

		for j := range ghIssue.Labels {
			lbl := ghIssue.Labels[j].String()

			if strings.HasPrefix(lbl, "component: ") {
				cmpName = lbl[11:]
			}

			if strings.HasPrefix(lbl, "type: ") {
				typeName = lbl[6:]
			}
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
			})

			cmp.Stats.Total++

			if ghIssue.ClosedAt != nil {
				cmp.Stats.Closed++
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
	}
}

type component struct {
	Name   string
	Issues issues
	Stats  struct {
		Closed int
		Total  int
	}
}

type issue struct {
	*github.Issue
	Type string
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
			<td colspan="3"><h3>{{ $ms.Title }}</h3></td>
			<td colspan="2" align="center"><h3>{{ $ms.Stats.Closed }}/{{ $ms.Stats.Total }}</h3></td>
		</tr>{{ range $j, $cmp := $ms.Components }}
		<tr>
			<td colspan="3"><h6>{{ $cmp.Name }}</h6></td>
			<td colspan="2" align="center"><h6>{{ $cmp.Stats.Closed }}/{{ $cmp.Stats.Total }}</h6></td>
		</tr>{{ range $k, $issue := $cmp.Issues }}
		<tr>
			<td><a href="{{ $issue.HTMLURL }}">#{{ $issue.Number }}</a></td>
			<td><kbd>{{ $issue.Type }}</kbd></td>
			<td>{{ $issue.Title }}</td>
			<td>{{ if $issue.Assignee }}<a href="{{ $issue.Assignee.HTMLURL }}"><img valign="middle" height="30" width="30" src="{{ $issue.Assignee.AvatarURL }} " /></a>{{ end }}</td>
			<td>{{ if $issue.ClosedAt }}:white_check_mark:{{ end }}</td>
		</tr>{{ end }}{{ end }}{{ end }}
	</tbody>
</table>
`))
