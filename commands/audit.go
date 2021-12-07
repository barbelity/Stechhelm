package commands

import (
	"errors"
	"fmt"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/jfrog/jfrog-cli-core/v2/artifactory/utils"
	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory"
	"os"
	"strings"
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
	repositoryDetails, err := serviceManager.GetAllRepositories()
	if err != nil {
		return err
	}

	// Get all repository configurations.
	localRemoteReposConfig := map[string]*CommonRepositoryDetails{}
	var repositoryConfigs []CommonRepositoryDetails
	for _, repositoryDetail := range *repositoryDetails {
		repositoryConfig := CommonRepositoryDetails{}
		err := serviceManager.GetRepository(repositoryDetail.Key, &repositoryConfig)
		if err != nil {
			return err
		}
		repositoryConfigs = append(repositoryConfigs, repositoryConfig)
		localRemoteReposConfig[repositoryConfig.Key] = &repositoryConfig
	}

	printAsTable(repositoryConfigs, localRemoteReposConfig, serviceManager)
	return nil
}

func printAsTable(repositoryConfigs []CommonRepositoryDetails, localRemoteReposConfig map[string]*CommonRepositoryDetails, serviceManager artifactory.ArtifactoryServicesManager) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"#", "Name", "Type", "Package type", "Include patterns", "Exclude patterns",
		"Priority Resolution", "Xray Index", "Is at Risk?"})
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 5, Align: text.AlignCenter},
		{Number: 6, Align: text.AlignCenter},
		{Number: 7, Align: text.AlignCenter},
		{Number: 8, Align: text.AlignCenter},
		{Number: 9, Align: text.AlignCenter},
	})
	riskCount := 0
	for i, repositoryConfig := range repositoryConfigs {
		riskString := "Safe"
		// Checking if Exclude & Include patterns are empty OR repo is local without priority resolution OR repo is not indexing by xray
		if strings.EqualFold(repositoryConfig.Rclass, "local") {
			if (repositoryConfig.PriorityResolution == false) || (repositoryConfig.XrayIndex == false) {
				riskString = "At risk"
				riskCount += 1
			}
		} else if strings.EqualFold(repositoryConfig.Rclass, "remote") {
			if ((repositoryConfig.ExcludesPattern == "") && (repositoryConfig.IncludesPattern == "**/*")) || (repositoryConfig.XrayIndex == false) {
				riskString = "At risk"
				riskCount += 1
			}
		} else if strings.EqualFold(repositoryConfig.Rclass, "virtual") {
			virtualRepositoryConfig := VirtualRepositoryDetails{}
			err := serviceManager.GetRepository(repositoryConfig.Key, &virtualRepositoryConfig)
			if err != nil {
				return err
			}
			isRepoSafe := checkVirtualRepoSafety(&virtualRepositoryConfig, localRemoteReposConfig)
			if !isRepoSafe {
				riskString = "At risk"
				riskCount += 1
			}
		}

		// Set output params
		incPatterns := "Not configured"
		if repositoryConfig.IncludesPattern != "**/*" {
			incPatterns = "Configured"
		}
		excPatterns := "Not configured"
		if repositoryConfig.ExcludesPattern != "" {
			excPatterns = "Configured"
		}

		if strings.EqualFold(repositoryConfig.Rclass, "virtual") {
			t.AppendRow(table.Row{i, repositoryConfig.Key, repositoryConfig.Rclass, repositoryConfig.PackageType,
				"-", "-", "-", "-", riskString})
		} else {
			t.AppendRow(table.Row{i, repositoryConfig.Key, repositoryConfig.Rclass, repositoryConfig.PackageType,
				incPatterns, excPatterns, repositoryConfig.PriorityResolution, repositoryConfig.XrayIndex, riskString})
		}
		t.AppendSeparator()
	}
	t.AppendFooter(table.Row{"", "", "", "", "", "", "", "Total at risk", riskCount})
	t.Render()
	return nil
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
