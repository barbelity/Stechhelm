package commands

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCheckVirtualRepoSafety(t *testing.T) {
	gb := &GraphBuilder{
		baseUrl:              "http://dummy.url",
		graphBuilderCommands: []string{},
		cypherCommands:       make(map[string]bool),
		repoToVirtualMapping: make(map[string]map[string]bool),
		allRepos:             make(map[string]*CommonRepositoryDetails),
	}
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
	gb.allRepos[safeLocalRepo1.Key] = safeLocalRepo1
	gb.allRepos[safeLocalRepo2.Key] = safeLocalRepo2

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

	result := gb.checkVirtualRepoSafety(virtualRepoConfig)
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
	gb.allRepos["remote1"] = unsafeRemoteRepo1
	virtualRepoConfig.Repositories = append(virtualRepoConfig.Repositories, "remote1")
	result = gb.checkVirtualRepoSafety(virtualRepoConfig)
	assert.False(t, result)
}

func TestLinkBinToAllVirtualRepos(t *testing.T) {
	gb := &GraphBuilder{
		baseUrl:              "http://dummy.url",
		graphBuilderCommands: []string{},
		cypherCommands:       make(map[string]bool),
		repoToVirtualMapping: make(map[string]map[string]bool),
		allRepos:             make(map[string]*CommonRepositoryDetails),
	}

	// Link artifact to repo.
	gb.linkBinToAllVirtualRepos("sha1", "repo1")
	assert.Equal(t, 1, len(gb.graphBuilderCommands))
	assert.Equal(t, 1, len(gb.cypherCommands))

	// Link same artifact to same repo - should have no change.
	gb.linkBinToAllVirtualRepos("sha1", "repo1")
	assert.Equal(t, 1, len(gb.graphBuilderCommands))
	assert.Equal(t, 1, len(gb.cypherCommands))

	// Link another artifact to same repo.
	gb.linkBinToAllVirtualRepos("sha2", "repo1")
	assert.Equal(t, 2, len(gb.graphBuilderCommands))
	assert.Equal(t, 2, len(gb.cypherCommands))

	// Link second artifact to another repo.
	gb.linkBinToAllVirtualRepos("sha2", "repo2")
	assert.Equal(t, 3, len(gb.graphBuilderCommands))
	assert.Equal(t, 3, len(gb.cypherCommands))

	// Link second artifact to first repo.
	gb.linkBinToAllVirtualRepos("sha2", "repo2")
	assert.Equal(t, 3, len(gb.graphBuilderCommands))
	assert.Equal(t, 3, len(gb.cypherCommands))
}

func TestCreateAqlQueryForChecksumRepositories(t *testing.T) {
	var inputTestCase = []struct {
		input    string
		expected string
	}{
		{"1234567890", "items.find({\"actual_sha1\": \"1234567890\"}).include(\"repo\")"},
		{"91d50642dd930e9542c39d36f0516d45f4e1af0d", "items.find({\"actual_sha1\": \"91d50642dd930e9542c39d36f0516d45f4e1af0d\"}).include(\"repo\")"},
	}
	for _, testCase := range inputTestCase {
		res := createAqlQueryForChecksumRepositories(testCase.input)
		if res != testCase.expected {
			t.Errorf("The expected output of createAqlQueryForChecksumRepositories(\"%s\") is %s. But the actual result is:%s", testCase.input, testCase.expected, res)
		}
	}
}
