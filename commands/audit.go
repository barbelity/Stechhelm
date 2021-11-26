package commands

import (
	"errors"
	"fmt"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/jfrog/jfrog-cli-core/v2/artifactory/utils"
	"github.com/jfrog/jfrog-cli-core/v2/common/commands"
	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory/services"
	clientutils "github.com/jfrog/jfrog-client-go/utils"
	//"github.com/jfrog/jfrog-client-go/utils/log"
	"github.com/jedib0t/go-pretty/v6/table"
	"os"
)

func GetAuditCommand() components.Command {
	return components.Command{
		Name:        "audit",
		Description: "Audit Artifactory repositories.",
		Aliases:     []string{"a"},
		Arguments:   getAuditArguments(),
		Flags:       getAuditFlags(),
		Action: func(c *components.Context) error {
			return auditCmd(c)
		},
	}
}

func auditCmd(c *components.Context) error {
	if len(c.Arguments) != 0 {
		return errors.New(fmt.Sprintf("Wrong number of arguments. Expected: 0, Received: %d", len(c.Arguments)))
	}
	rtDetails, err := getRtDetails(c)
	if err != nil {
		return err
	}
	return doAudit(rtDetails)
}

func doAudit(artifactoryDetails *config.ServerDetails) error {
	// Create service-manager.
	serviceManager, err := utils.CreateServiceManager(artifactoryDetails, -1, false)
	if err != nil {
		return err
	}

	// Get all repositories.
	jfrogClient := serviceManager.Client()
	repositoriesService := services.NewRepositoriesService(jfrogClient)
	repositoriesService.ArtDetails = serviceManager.GetConfig().GetServiceDetails()
	repositoryDetails, err := repositoriesService.GetAll()
	if err != nil {
		return err
	}

	// Get all repository configurations.
	var repositoryConfigs []AuditRepositoryDetails
	for _, repositoryDetail := range *repositoryDetails {
		repositoryConfig := AuditRepositoryDetails{}
		err := repositoriesService.Get(repositoryDetail.Key, &repositoryConfig) // TODO: Bar - Exclude/Include patterns are always empty, even if there are some
		if err != nil {
			return err
		}
		repositoryConfigs = append(repositoryConfigs, repositoryConfig)
	}

	//log.Info(fmt.Sprintf("Configurations: %#v", repositoryConfigs))
	printAsTable(repositoryConfigs)
	return nil
}

func printAsTable(repositoryConfigs []AuditRepositoryDetails) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Type", "Package type", "Include patterns", "Exclude patterns",
		"Priority Resolution", "Xray Index", "Risk?"})
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 5, Align: text.AlignCenter},
		{Number: 6, Align: text.AlignCenter},
		{Number: 7, Align: text.AlignCenter},
		{Number: 8, Align: text.AlignCenter},
		{Number: 9, Align: text.AlignCenter},
	})
	riskCount := 0
	for i, repositoryConfig := range repositoryConfigs {
		risk := false
		// Checking if Exclude & Include patterns are empty OR repo is local without priority resolution OR repo is not indexing by xray
		if ((repositoryConfig.ExcludePattern == "") && (repositoryConfig.IncludePattern == "")) || ((repositoryConfig.PriorityResolution == false) && (repositoryConfig.Rclass == "local")) || (repositoryConfig.XrayIndex == false) {
			risk = true
			riskCount += 1
		}

		// Set output params
		incPatterns := "X"
		if repositoryConfig.IncludePattern != "" {
			incPatterns = "V"
		}
		excPatterns := "X"
		if repositoryConfig.IncludePattern != "" {
			excPatterns = "V"
		}

		t.AppendRow(table.Row{i, repositoryConfig.Key, repositoryConfig.Rclass, repositoryConfig.PackageType,
			incPatterns, excPatterns, repositoryConfig.PriorityResolution, repositoryConfig.XrayIndex,
			risk})
		t.AppendSeparator()
	}
	t.AppendFooter(table.Row{"", "", "", "", "", "", "", "Total in risk", riskCount})
	t.Render()
}

type AuditRepositoryDetails struct {
	Key                string
	Rclass             string
	PackageType        string
	IncludePattern     string
	ExcludePattern     string
	XrayIndex          bool
	PriorityResolution bool
}

func getAuditArguments() []components.Argument {
	return []components.Argument{}
}

func getAuditFlags() []components.Flag {
	return []components.Flag{
		components.StringFlag{
			Name:        "server-id",
			Description: "Artifactory server ID configured using the config command.",
		},
	}
}

// Returns the Artifactory Details of the provided server-id, or the default one.
func getRtDetails(c *components.Context) (*config.ServerDetails, error) {
	serverId := c.GetStringFlagValue("server-id")
	details, err := commands.GetConfig(serverId, false)
	if err != nil {
		return nil, err
	}
	if details.Url == "" {
		return nil, errors.New("no server-id was found, or the server-id has no url")
	}
	details.Url = clientutils.AddTrailingSlashIfNeeded(details.Url)
	err = config.CreateInitialRefreshableTokensIfNeeded(details)
	if err != nil {
		return nil, err
	}
	return details, nil
}
