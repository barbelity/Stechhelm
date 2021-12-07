package commands

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckVirtualRepoSafety(t *testing.T) {
	allRepos := make(map[string]*CommonRepositoryDetails)
	safeLocalRepo1 := &CommonRepositoryDetails{
		Key:                "local1",
		Rclass:             "local",
		XrayIndex:          true,
		PackageType:        "go",
		IncludesPattern:    "abc*",
		ExcludesPattern:    "efg*",
		PriorityResolution: true,
	}
	safeLocalRepo2 := &CommonRepositoryDetails{
		Key:                "local2",
		Rclass:             "local",
		XrayIndex:          true,
		PackageType:        "maven",
		IncludesPattern:    "abc*",
		ExcludesPattern:    "efg*",
		PriorityResolution: true,
	}
	allRepos[safeLocalRepo1.Key] = safeLocalRepo1
	allRepos[safeLocalRepo2.Key] = safeLocalRepo2

	// Test case of virtual containing safe repos.
	virtualRepoConfig := &VirtualRepositoryDetails{
		CommonRepositoryDetails: CommonRepositoryDetails{
			Key:                "repo1",
			Rclass:             "virtual",
			XrayIndex:          false,
			PackageType:        "maven",
			IncludesPattern:    "**/*",
			ExcludesPattern:    "",
			PriorityResolution: false,
		},
		Repositories: []string{"local1", "local2"},
	}

	result := checkVirtualRepoSafety(virtualRepoConfig, allRepos)
	assert.True(t, result)

	// Test case of virtual containing 2 safe local repos and 1 unsafe remote repo.
	unsafeRemoteRepo1 := &CommonRepositoryDetails{
		Key:             "remote1",
		Rclass:          "remote",
		XrayIndex:       true,
		PackageType:     "maven",
		IncludesPattern: "**/*",
		ExcludesPattern: "",
	}
	allRepos["remote1"] = unsafeRemoteRepo1
	virtualRepoConfig.Repositories = append(virtualRepoConfig.Repositories, "remote1")
	result = checkVirtualRepoSafety(virtualRepoConfig, allRepos)
	assert.False(t, result)
}
