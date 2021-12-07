package commands

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLinkBinToAllVirtualRepos(t *testing.T) {
	gb := &GraphBuilder{
		baseUrl:              "http://dummy.url",
		graphBuilderCommands: []string{},
		cypherCommands:       make(map[string]bool),
		repoToVirtualMapping: make(map[string]map[string]bool),
		allRepos:             make(map[string]*CommonRepositoryDetails),
	}

	// Link artifact to repo.
	gb.linkBinToRepos("sha1", "repo1")
	assert.Equal(t, 1, len(gb.graphBuilderCommands))
	assert.Equal(t, 1, len(gb.cypherCommands))

	// Link same artifact to same repo - should have no change.
	gb.linkBinToRepos("sha1", "repo1")
	assert.Equal(t, 1, len(gb.graphBuilderCommands))
	assert.Equal(t, 1, len(gb.cypherCommands))

	// Link another artifact to same repo.
	gb.linkBinToRepos("sha2", "repo1")
	assert.Equal(t, 2, len(gb.graphBuilderCommands))
	assert.Equal(t, 2, len(gb.cypherCommands))

	// Link second artifact to another repo.
	gb.linkBinToRepos("sha2", "repo2")
	assert.Equal(t, 3, len(gb.graphBuilderCommands))
	assert.Equal(t, 3, len(gb.cypherCommands))

	// Link second artifact to first repo.
	gb.linkBinToRepos("sha2", "repo2")
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
