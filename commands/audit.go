package commands

import (
	"errors"
	"fmt"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/jfrog/jfrog-cli-core/v2/artifactory/utils"
	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory/services"
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
		err := repositoriesService.Get(repositoryDetail.Key, &repositoryConfig)
		if err != nil {
			return err
		}
		repositoryConfigs = append(repositoryConfigs, repositoryConfig)
	}

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
		if ((repositoryConfig.ExcludesPattern == "") && (repositoryConfig.IncludesPattern == "**/*")) ||
			((repositoryConfig.PriorityResolution == false) && (repositoryConfig.Rclass == "local")) || (repositoryConfig.XrayIndex == false) {
			risk = true
			riskCount += 1
		}

		// Set output params
		incPatterns := "X"
		if repositoryConfig.IncludesPattern != "**/*" {
			incPatterns = "V"
		}
		excPatterns := "X"
		if repositoryConfig.ExcludesPattern != "" {
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
	Key                string `json:"key"`
	Rclass             string `json:"rclass"`
	PackageType        string `json:"packageType"`
	IncludesPattern    string `json:"includesPattern"`
	ExcludesPattern    string `json:"excludesPattern"`
	XrayIndex          bool   `json:"xrayIndex"`
	PriorityResolution bool   `json:"priorityResolution"`
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
