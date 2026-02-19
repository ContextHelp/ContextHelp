package printer

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5C5CFF")).
			Padding(0, 1)

	cellStyle = lipgloss.NewStyle().Padding(0, 1)

	borderColor = lipgloss.Color("238")
)

func renderTable(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No results")
	}

	return table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(borderColor)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		}).
		Render()
}

func PrintPipelines(pipelines []Pipeline) {
	if len(pipelines) == 0 {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No pipelines"))
		return
	}

	var rows [][]string
	for _, p := range pipelines {
		archived := "false"
		if p.Archived {
			archived = "true"
		}
		rows = append(rows, []string{p.Name, truncate(p.Description, 40), fmt.Sprintf("%d", p.Steps), archived})
	}
	fmt.Println(renderTable([]string{"NAME", "DESCRIPTION", "STEPS", "ARCHIVED"}, rows))
}

func PrintSteps(steps []Step) {
	if len(steps) == 0 {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No steps"))
		return
	}

	var rows [][]string
	for _, s := range steps {
		version := "1.0.0"
		if s.Version != "" {
			version = s.Version
		}
		rows = append(rows, []string{s.Name, s.Source, version})
	}
	fmt.Println(renderTable([]string{"NAME", "SOURCE", "VERSION"}, rows))
}

func PrintRegistries(registries []Registry) {
	if len(registries) == 0 {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No registries"))
		return
	}

	var rows [][]string
	for _, r := range registries {
		autoUpdate := "false"
		if r.AutoUpdate {
			autoUpdate = "true"
		}
		rows = append(rows, []string{r.URL, r.Version, autoUpdate, r.LastFetched})
	}
	fmt.Println(renderTable([]string{"URL", "VERSION", "AUTO-UPDATE", "LAST FETCHED"}, rows))
}

func PrintJobs(jobs []Job) {
	if len(jobs) == 0 {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No jobs"))
		return
	}

	var rows [][]string
	for _, j := range jobs {
		rows = append(rows, []string{truncate(j.ID, 8), j.Type, StatusStyle(j.Status), j.Pipeline, j.Created})
	}
	fmt.Println(renderTable([]string{"ID", "TYPE", "STATUS", "PIPELINE", "CREATED"}, rows))
}

func StatusStyle(status string) string {
	var color lipgloss.Color
	switch strings.ToLower(status) {
	case "completed":
		color = lipgloss.Color("2")
	case "failed":
		color = lipgloss.Color("1")
	case "running", "processing":
		color = lipgloss.Color("3")
	case "pending":
		color = lipgloss.Color("8")
	default:
		color = lipgloss.Color("7")
	}
	return lipgloss.NewStyle().Foreground(color).Render(status)
}

func PrintSuccess(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		SetString("✓")
	fmt.Printf("%s %s\n", style, msg)
}

func PrintError(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("1")).
		SetString("✗")
	fmt.Fprintf(os.Stderr, "%s %s\n", style, msg)
}

func PrintInfo(msg string) {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("4")).
		SetString("ℹ")
	fmt.Printf("%s %s\n", style, msg)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

type Pipeline struct {
	Name        string
	Description string
	Steps       int
	Archived    bool
}

type Step struct {
	Name    string
	Source  string
	Version string
}

type Registry struct {
	URL         string
	Version     string
	AutoUpdate  bool
	LastFetched string
}

type Job struct {
	ID       string
	Type     string
	Status   string
	Pipeline string
	Created  string
}
