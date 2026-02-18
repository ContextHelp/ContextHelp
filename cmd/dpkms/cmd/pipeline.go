package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var apiClient *APIClient

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Manage ingestion pipelines",
}

var pipelineCreateCmd = &cobra.Command{
	Use:     "create [flags] <file>",
	Aliases: []string{"add"},
	Short:   "Create a custom pipeline from config file",
	Args:    cobra.ExactArgs(1),
	RunE:    runPipelineCreate,
}

var pipelineRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"del"},
	Short:   "Delete a custom pipeline",
	Args:    cobra.ExactArgs(1),
	RunE:    runPipelineRemove,
}

var pipelineListCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls", "all"},
	Short:   "List pipelines with optional filters",
	Args:    cobra.NoArgs,
	RunE:    runPipelineList,
}

var pipelineShowCmd = &cobra.Command{
	Use:     "show <name>",
	Aliases: []string{"view"},
	Short:   "Show pipeline details",
	Args:    cobra.ExactArgs(1),
	RunE:    runPipelineShow,
}

var pipelineArchiveCmd = &cobra.Command{
	Use:     "archive <name>",
	Aliases: []string{"disable"},
	Short:   "Archive a pipeline",
	Args:    cobra.ExactArgs(1),
	RunE:    runPipelineArchive,
}

var pipelineUnarchiveCmd = &cobra.Command{
	Use:     "unarchive <name>",
	Aliases: []string{"enable"},
	Short:   "Unarchive a pipeline",
	Args:    cobra.ExactArgs(1),
	RunE:    runPipelineUnarchive,
}

var pipelineEnqueueCmd = &cobra.Command{
	Use:     "enqueue [flags] [content|file|-]",
	Aliases: []string{"push"},
	Short:   "Enqueue content for processing",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runPipelineEnqueue,
}

var stepCmd = &cobra.Command{
	Use:   "step",
	Short: "Manage pipeline steps",
}

var stepListCmd = &cobra.Command{
	Use:   "list [flags]",
	Short: "List available steps",
	Args:  cobra.NoArgs,
	RunE:  runStepList,
}

var stepInstallCmd = &cobra.Command{
	Use:   "install <name> --registry <url>",
	Short: "Install step from registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runStepInstall,
}

var stepUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall step",
	Args:  cobra.ExactArgs(1),
	RunE:  runStepUninstall,
}

var stepRegistryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage step registries",
}

var stepRegistryAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a registry",
	Args:  cobra.ExactArgs(2),
	RunE:  runStepRegistryAdd,
}

var stepRegistryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registries",
	Args:  cobra.NoArgs,
	RunE:  runStepRegistryList,
}

var stepRegistryUpdateCmd = &cobra.Command{
	Use:   "update <url>",
	Short: "Update registry manifest",
	Args:  cobra.ExactArgs(1),
	RunE:  runStepRegistryUpdate,
}

var stepRegistryAutoUpdateCmd = &cobra.Command{
	Use:   "autoupdate <url> [--enable|--disable]",
	Short: "Configure registry auto-update",
	Args:  cobra.ExactArgs(1),
	RunE:  runStepRegistryAutoUpdate,
}

var (
	pipelineNameFilter      string
	pipelineIncludeArchived bool
	pipelineOnlyArchived    bool
	pipelineShowRaw         bool
)

var (
	pipelineType     string
	pipelinePipeline string
	pipelineWait     bool
)

var (
	stepSource   string
	stepRegistry string
)

var (
	registryAutoUpdateEnable  bool
	registryAutoUpdateDisable bool
)

func init() {
	rootCmd.AddCommand(pipelineCmd)
	pipelineCmd.AddCommand(pipelineCreateCmd)
	pipelineCmd.AddCommand(pipelineRemoveCmd)
	pipelineCmd.AddCommand(pipelineListCmd)
	pipelineCmd.AddCommand(pipelineShowCmd)
	pipelineCmd.AddCommand(pipelineArchiveCmd)
	pipelineCmd.AddCommand(pipelineUnarchiveCmd)
	pipelineCmd.AddCommand(pipelineEnqueueCmd)

	pipelineCmd.AddCommand(stepCmd)
	stepCmd.AddCommand(stepListCmd)
	stepCmd.AddCommand(stepInstallCmd)
	stepCmd.AddCommand(stepUninstallCmd)
	stepCmd.AddCommand(stepRegistryCmd)
	stepRegistryCmd.AddCommand(stepRegistryAddCmd)
	stepRegistryCmd.AddCommand(stepRegistryListCmd)
	stepRegistryCmd.AddCommand(stepRegistryUpdateCmd)
	stepRegistryCmd.AddCommand(stepRegistryAutoUpdateCmd)

	pipelineListCmd.Flags().StringVar(&pipelineNameFilter, "name", "", "filter by name")
	pipelineListCmd.Flags().BoolVar(&pipelineIncludeArchived, "include-archived", false, "include archived pipelines")
	pipelineListCmd.Flags().BoolVar(&pipelineOnlyArchived, "only-archived", false, "show only archived pipelines")

	pipelineShowCmd.Flags().BoolVar(&pipelineShowRaw, "raw", false, "output raw JSON")

	pipelineEnqueueCmd.Flags().StringVar(&pipelineType, "type", "text", "content type")
	pipelineEnqueueCmd.Flags().StringVar(&pipelinePipeline, "pipeline", "", "pipeline name")
	pipelineEnqueueCmd.Flags().BoolVar(&pipelineWait, "wait", false, "wait for job completion")

	stepListCmd.Flags().StringVar(&stepSource, "source", "", "filter by source")

	stepInstallCmd.Flags().StringVar(&stepRegistry, "registry", "", "registry URL")

	stepRegistryAutoUpdateCmd.Flags().BoolVar(&registryAutoUpdateEnable, "enable", false, "enable auto-update")
	stepRegistryAutoUpdateCmd.Flags().BoolVar(&registryAutoUpdateDisable, "disable", false, "disable auto-update")
}

func initPipelineClient(serverURL string) {
	apiClient = NewAPIClient(serverURL)
}

func runPipelineCreate(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	var req service.CreatePipelineRequest
	ext := strings.ToLower(filepath.Ext(filePath))

	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	switch ext {
	case ".json":
		var config map[string]any
		if err := json.Unmarshal(content, &config); err != nil {
			return err
		}
		req.Name = config["name"].(string)
		req.Description = config["description"].(string)
		stepsJSON, _ := json.Marshal(config["steps"])
		req.Steps = string(stepsJSON)
	case ".yaml", ".yml":
		var config map[string]any
		if err := yaml.Unmarshal(content, &config); err != nil {
			return err
		}
		req.Name = config["name"].(string)
		req.Description = config["description"].(string)
		stepsJSON, _ := json.Marshal(config["steps"])
		req.Steps = string(stepsJSON)
	case ".toml":
		var config map[string]any
		if err := toml.Unmarshal(content, &config); err != nil {
			return err
		}
		req.Name = config["name"].(string)
		req.Description = config["description"].(string)
		stepsJSON, _ := json.Marshal(config["steps"])
		req.Steps = string(stepsJSON)
	default:
		return fmt.Errorf("unsupported config format: %s", ext)
	}

	if pipelineShowRaw {
		return printJSON(req)
	}

	id, err := apiClient.CreatePipeline(req)
	if err != nil {
		return err
	}

	printJSON(map[string]string{"id": id})
	return nil
}

func runPipelineRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	return apiClient.DeletePipeline(name)
}

func runPipelineList(cmd *cobra.Command, _ []string) error {
	filter := storage.PipelineFilter{
		Name:            pipelineNameFilter,
		IncludeArchived: pipelineIncludeArchived,
		OnlyArchived:    pipelineOnlyArchived,
	}

	pipelines, _, err := apiClient.ListPipelines(filter)
	if err != nil {
		return err
	}

	if pipelineShowRaw {
		return printJSON(pipelines)
	}

	return printTable(pipelines)
}

func runPipelineShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	pipeline, err := apiClient.GetPipeline(name)
	if err != nil {
		return err
	}

	if pipelineShowRaw {
		return printJSON(pipeline)
	}

	return printPipelineDetails(pipeline)
}

func runPipelineArchive(cmd *cobra.Command, args []string) error {
	name := args[0]
	return apiClient.ArchivePipeline(name)
}

func runPipelineUnarchive(cmd *cobra.Command, args []string) error {
	name := args[0]
	return apiClient.UnarchivePipeline(name)
}

func runPipelineEnqueue(cmd *cobra.Command, args []string) error {
	var content string
	if len(args) > 0 {
		if args[0] == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			content = string(b)
		} else {
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			content = string(b)
		}
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		content = string(b)
	}

	req := service.AnalyzeRequest{
		Content:  content,
		Type:     pipelineType,
		Pipeline: pipelinePipeline,
	}

	jobID, err := apiClient.Enqueue(req)
	if err != nil {
		return err
	}

	if pipelineWait {
		return waitForJob(jobID)
	}

	printJSON(map[string]string{"job_id": jobID})
	return nil
}

func runStepList(cmd *cobra.Command, _ []string) error {
	steps, _, err := apiClient.ListSteps(stepSource)
	if err != nil {
		return err
	}

	if pipelineShowRaw {
		return printJSON(steps)
	}

	return printStepTable(steps)
}

func runStepInstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	if stepRegistry == "" {
		return fmt.Errorf("--registry is required")
	}
	return apiClient.InstallStep(name, stepRegistry)
}

func runStepUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	return apiClient.UninstallStep(name)
}

func runStepRegistryAdd(cmd *cobra.Command, args []string) error {
	url := args[1]
	return apiClient.FetchRegistry(url)
}

func runStepRegistryList(cmd *cobra.Command, _ []string) error {
	registries, _, err := apiClient.ListRegistries()
	if err != nil {
		return err
	}

	if pipelineShowRaw {
		return printJSON(registries)
	}

	return printRegistryTable(registries)
}

func runStepRegistryUpdate(cmd *cobra.Command, args []string) error {
	url := args[0]
	return apiClient.UpdateRegistry(url)
}

func runStepRegistryAutoUpdate(cmd *cobra.Command, args []string) error {
	url := args[0]
	if registryAutoUpdateEnable {
		return apiClient.ConfigureAutoUpdate(url, true)
	}
	if registryAutoUpdateDisable {
		return apiClient.ConfigureAutoUpdate(url, false)
	}
	return fmt.Errorf("must specify --enable or --disable")
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func printTable(pipelines []*storage.Pipeline) error {
	fmt.Println("NAME\tDESCRIPTION\tSTEPS\tARCHIVED")
	for _, p := range pipelines {
		fmt.Printf("%s\t%s\t%d\t%t\n", p.Name, p.Description, len(p.Steps), p.Archived)
	}
	return nil
}

func printPipelineDetails(p *storage.Pipeline) error {
	fmt.Printf("Name: %s\n", p.Name)
	fmt.Printf("Description: %s\n", p.Description)
	fmt.Printf("Archived: %t\n", p.Archived)
	fmt.Printf("Built-in: %t\n", p.IsBuiltIn)
	fmt.Println("Steps:")
	for _, step := range p.Steps {
		fmt.Printf("  - %s\n", step.Name)
	}
	return nil
}

func printStepTable(steps []*storage.RegisteredStep) error {
	fmt.Println("NAME\tSOURCE\tVERSION")
	for _, s := range steps {
		version := "1.0.0"
		if s.Metadata != nil {
			version = s.Metadata.Version
		}
		fmt.Printf("%s\t%s\t%s\n", s.Name, s.Source, version)
	}
	return nil
}

func printRegistryTable(registries []*storage.RegistryCache) error {
	fmt.Println("URL\tVERSION\tAUTO-UPDATE\tLAST FETCHED")
	for _, r := range registries {
		version := "unknown"
		if r.Manifest != nil {
			version = r.Manifest.Version
		}
		fmt.Printf("%s\t%s\t%t\t%s\n", r.RegistryURL, version, r.AutoUpdate, r.LastFetched.Format(time.RFC3339))
	}
	return nil
}

func waitForJob(jobID string) error {
	for {
		job, err := apiClient.GetJob(jobID)
		if err != nil {
			return err
		}
		if job.Status == storage.JobCompleted {
			return nil
		}
		if job.Status == storage.JobFailed {
			return fmt.Errorf("job failed: %s", job.Error)
		}
		time.Sleep(1 * time.Second)
	}
}
