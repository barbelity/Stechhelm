package commands

import (
	"errors"
	"github.com/jfrog/jfrog-cli-core/v2/common/commands"
	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	clientutils "github.com/jfrog/jfrog-client-go/utils"
)

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

type CommonRepositoryDetails struct {
	Key                string `json:"key"`
	Rclass             string `json:"rclass"`
	XrayIndex          bool   `json:"xrayIndex"`
	PackageType        string `json:"packageType"`
	IncludesPattern    string `json:"includesPattern"`
	ExcludesPattern    string `json:"excludesPattern"`
	PriorityResolution bool   `json:"priorityResolution"`
	IsSafe             bool
}

type VirtualRepositoryDetails struct {
	CommonRepositoryDetails
	Repositories []string `json:"repositories"`
}
