package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jfrog/jfrog-cli-core/v2/artifactory/utils"
	"github.com/jfrog/jfrog-cli-core/v2/plugins/components"
	"github.com/jfrog/jfrog-cli-core/v2/utils/config"
	"github.com/jfrog/jfrog-client-go/artifactory"
	"github.com/jfrog/jfrog-client-go/artifactory/buildinfo"
	"github.com/jfrog/jfrog-client-go/artifactory/services"
	clientutils "github.com/jfrog/jfrog-client-go/utils"
	"github.com/jfrog/jfrog-client-go/utils/errorutils"
	"github.com/jfrog/jfrog-client-go/utils/io/httputils"
	"github.com/jfrog/jfrog-client-go/utils/log"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func GetGraphCommand() components.Command {
	return components.Command{
		Name:        "graph",
		Description: "Create a graph.",
		Aliases:     []string{"g"},
		Arguments:   getAuditArguments(),
		Flags:       getAuditFlags(),
		Action: func(c *components.Context) error {
			return graphCmd(c)
		},
	}
}

func graphCmd(c *components.Context) error {
	if len(c.Arguments) != 0 {
		return errors.New(fmt.Sprintf("Wrong number of arguments. Expected: 0, Received: %d", len(c.Arguments)))
	}
	rtDetails, err := getRtDetails(c)
	if err != nil {
		return err
	}

	graphBuilder := &GraphBuilder{
		rtDetails:            rtDetails,
		cypherCommands:       make(map[string]bool),
		graphBuilderCommands: &strings.Builder{},
		repoToVirtualMapping: make(map[string]map[string]bool),
	}
	// Initialize graphBuilder fields.
	graphBuilder.serviceManager, err = utils.CreateServiceManager(graphBuilder.rtDetails, -1, false)
	if err != nil {
		return err
	}
	serviceDetails := graphBuilder.serviceManager.GetConfig().GetServiceDetails()
	graphBuilder.clientDetails = serviceDetails.CreateHttpClientDetails()
	graphBuilder.baseUrl = serviceDetails.GetUrl()

	return graphBuilder.makeGraph()
}

type GraphBuilder struct {
	rtDetails            *config.ServerDetails
	cypherCommands       map[string]bool
	graphBuilderCommands *strings.Builder
	repoToVirtualMapping map[string]map[string]bool
	serviceManager       artifactory.ArtifactoryServicesManager
	clientDetails        httputils.HttpClientDetails
	baseUrl              string
}

func (gb GraphBuilder) makeGraph() error {

	startTime := time.Now()
	// TODO: Create a file for the command output.

	// Create repositories relations.
	err := gb.CreateRepositoriesGraphRelations()
	if err != nil {
		return err
	}

	// Handle builds.
	builds, err := gb.getAllBuilds()
	if err != nil {
		return err
	}
	if len(builds) == 0 {
		return nil
	}
	err = gb.handleBuilds(builds)
	if err != nil {
		return err
	}

	// TODO: Execute graph driver

	// Create temp file
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	fileName := "stechhelm-output-" + timestamp + ".txt"
	err = ioutil.WriteFile(fileName, []byte(gb.graphBuilderCommands.String()), 0644)
	if err != nil {
		log.Error("Failed creating file for output.")
	}

	log.Info(fmt.Sprintf("Graph commands:\n%v", gb.graphBuilderCommands.String()))

	endTime := time.Now()
	log.Info(fmt.Sprintf("Graph creation took: %f seconds", endTime.Sub(startTime).Seconds()))

	return nil
}

func (gb GraphBuilder) handleBuilds(builds []Build) error {
	visitedChecksums := map[string]bool{}
	for _, build := range builds {
		buildName := strings.TrimPrefix(build.Uri, "/")
		buildInfoParms := services.BuildInfoParams{
			BuildName:   buildName,
			BuildNumber: "LATEST",
		}
		buildInfoObj, buildFound, err := gb.serviceManager.GetBuildInfo(buildInfoParms)
		if err != nil {
			log.Error(fmt.Sprintf("an error has occurred when fetching latest build for %s: %s", buildName, err.Error()))
			continue
		}
		if buildInfoObj == nil || !buildFound {
			log.Info(fmt.Sprintf("Latest Build could not be found, name: %s", buildName))
			continue
		}
		buildInfo := buildInfoObj.BuildInfo
		if len(buildInfo.Modules) == 0 {
			log.Info(fmt.Sprintf("No modules found for build name: %s, number: %s", buildInfo.Name, buildInfo.Number))
			continue
		}

		log.Info(fmt.Sprintf("Handling modules of build name: %s, number: %s", buildInfo.Name, buildInfo.Number))
		// Handle modules.
		for _, module := range buildInfo.Modules {
			// Handle dependencies.
			log.Info("handling module id: " + module.Id)
			for _, dependency := range module.Dependencies {
				if dependency.Checksum == nil || dependency.Checksum.Sha1 == "" {
					continue
				}
				err := gb.handleDependency(&dependency, &buildInfo, visitedChecksums)
				if err != nil {
					log.Info(fmt.Sprintf("an error has ocurred when handling build: %s dependency: %s", buildName, dependency.Sha1), err)
				}
			}

			for _, artifact := range module.Artifacts {
				if artifact.Checksum == nil || artifact.Checksum.Sha1 == "" {
					continue
				}
				err = gb.handleArtifact(&artifact, &buildInfo, visitedChecksums)
				if err != nil {
					log.Info(fmt.Sprintf("an error has ocurred when handling build: %s artifact: %s", buildName, artifact.Sha1), err)
				}
			}
		}
	}
	return nil
}

func (gb GraphBuilder) handleArtifact(artifact *buildinfo.Artifact, buildInfo *buildinfo.BuildInfo,
	visitedChecksums map[string]bool) error {
	gb.graphCreateRelationshipBuildToArtifact(buildInfo.Name, buildInfo.Number, artifact.Sha1)
	if _, ok := visitedChecksums[artifact.Sha1]; ok {
		return nil
	}
	repoResults, err := gb.getRepositoryListBySha1(artifact.Sha1)
	if err != nil {
		log.Info(fmt.Sprintf("Could not find repositories for sha1 %s: %s", artifact.Sha1, err.Error()))
		return nil
	}
	visitedChecksums[artifact.Sha1] = true
	for _, repoResult := range repoResults {
		repoName := repoResult.Repo
		gb.linkBinToAllVirtualRepos(artifact.Sha1, repoName)
	}
	return nil
}

func (gb GraphBuilder) handleDependency(dependency *buildinfo.Dependency, buildInfo *buildinfo.BuildInfo,
	visitedChecksums map[string]bool) error {
	gb.graphCreateRelationshipDependencyToBuild(buildInfo.Name, buildInfo.Number, dependency.Sha1)
	if _, ok := visitedChecksums[dependency.Sha1]; ok {
		return nil
	}
	repoResults, err := gb.getRepositoryListBySha1(dependency.Sha1)
	if err != nil {
		log.Info(fmt.Sprintf("Could not find repositories for sha1 %s: %s", dependency.Sha1, err.Error()))
		return nil
	}
	visitedChecksums[dependency.Sha1] = true
	for _, repoResult := range repoResults {
		repoName := repoResult.Repo
		gb.linkBinToAllVirtualRepos(dependency.Sha1, repoName)
	}
	return nil
}

func (gb GraphBuilder) getRepositoryListBySha1(sha1 string) ([]Result, error) {
	var stream io.ReadCloser
	stream, err := gb.serviceManager.Aql(createAqlQueryForChecksumRepositories(sha1))
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var result []byte
	result, err = ioutil.ReadAll(stream)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	parsedResults := Sha1AqlResults{}
	if err = json.Unmarshal(result, &parsedResults); err != nil {
		return nil, err
	}
	if len(parsedResults.Results) == 0 {
		return nil, nil
	}
	return parsedResults.Results, nil
}

func createAqlQueryForChecksumRepositories(sha1 string) string {
	itemsPart :=
		`items.find({` +
			`"actual_sha1": "%s"` +
			`}).include("repo")`
	return fmt.Sprintf(itemsPart, sha1)
}

type Sha1AqlResults struct {
	Results []Result `json:"results"`
}

type Result struct {
	Repo string `json:"repo"`
}

type Builds struct {
	Builds []Build `json:"builds"`
}

type Build struct {
	Uri string `json:"uri"`
}

func (gb GraphBuilder) getAllBuilds() ([]Build, error) {
	resp, respBody, _, err := gb.serviceManager.Client().SendGet(fmt.Sprintf("%s%s", gb.baseUrl, "api/build"), true, &gb.clientDetails)
	if err != nil {
		return nil, err
	}
	if err = errorutils.CheckResponseStatus(resp, http.StatusOK); err != nil {
		return nil, errorutils.GenerateResponseError(resp.Status, clientutils.IndentJson(respBody))
	}

	var allBuilds Builds
	err = json.Unmarshal(respBody, &allBuilds)
	if err != nil {
		return nil, err
	}

	return allBuilds.Builds, nil
}

func (gb GraphBuilder) CreateRepositoriesGraphRelations() error {
	err := gb.handleLocalRepositories()
	if err != nil {
		return err
	}
	err = gb.handleRemoteRepositories()
	if err != nil {
		return err
	}
	err = gb.handleVirtualRepositories()
	if err != nil {
		return err
	}
	return nil
}

func (gb GraphBuilder) handleVirtualRepositories() error {
	params := services.NewRepositoriesFilterParams()
	params.RepoType = "virtual"
	virtualReposDetails, err := gb.serviceManager.GetAllRepositoriesFiltered(params)
	if err != nil {
		return err
	}
	for _, repositoryDetail := range *virtualReposDetails {
		repositoryConfig := VirtualRepositoryDetails{}
		err := gb.serviceManager.GetRepository(repositoryDetail.Key, &repositoryConfig)
		if err != nil {
			return err
		}
		gb.graphCreateRepoNode(repositoryConfig.Key, "VIRTUAL", repositoryConfig.PriorityResolution,
			repositoryConfig.IncludesPattern != "**/*", repositoryConfig.ExcludesPattern != "", repositoryConfig.XrayIndex)

		// Populate repositories to virtuals map.
		for _, linkedRepo := range repositoryConfig.Repositories {
			gb.graphCreateRelationshipVirtualToLocalOrRemote(repositoryConfig.Key, linkedRepo)
			if linkedRepoVirtuals, ok := gb.repoToVirtualMapping[linkedRepo]; ok {
				// linkedRepo has a list of virtuals.
				if _, ok2 := linkedRepoVirtuals[repositoryConfig.Key]; !ok2 {
					// Add virtual repo.
					linkedRepoVirtuals[repositoryConfig.Key] = true
				}
			} else {
				// linkedRepo doesn't have a list of virtuals.
				gb.repoToVirtualMapping[linkedRepo] = map[string]bool{repositoryConfig.Key: true}
			}
		}
	}
	return nil
}

func (gb GraphBuilder) handleLocalRepositories() error {
	params := services.NewRepositoriesFilterParams()
	params.RepoType = "local"
	localReposDetails, err := gb.serviceManager.GetAllRepositoriesFiltered(params)
	if err != nil {
		return err
	}
	for _, repositoryDetail := range *localReposDetails {
		repositoryConfig := CommonRepositoryDetails{}
		err := gb.serviceManager.GetRepository(repositoryDetail.Key, &repositoryConfig)
		if err != nil {
			return err
		}
		gb.graphCreateRepoNode(repositoryConfig.Key, "LOCAL", repositoryConfig.PriorityResolution,
			repositoryConfig.IncludesPattern != "**/*", repositoryConfig.ExcludesPattern != "", repositoryConfig.XrayIndex)
	}
	return nil
}

func (gb GraphBuilder) handleRemoteRepositories() error {
	params := services.NewRepositoriesFilterParams()
	params.RepoType = "remote"
	remoteReposDetails, err := gb.serviceManager.GetAllRepositoriesFiltered(params)
	if err != nil {
		return err
	}
	for _, repositoryDetail := range *remoteReposDetails {
		repositoryConfig := CommonRepositoryDetails{}
		err := gb.serviceManager.GetRepository(repositoryDetail.Key, &repositoryConfig)
		if err != nil {
			return err
		}
		gb.graphCreateRepoNode(repositoryConfig.Key, "REMOTE", repositoryConfig.PriorityResolution,
			repositoryConfig.IncludesPattern != "**/*", repositoryConfig.ExcludesPattern != "", repositoryConfig.XrayIndex)
	}
	return nil
}

func (gb GraphBuilder) linkBinToAllVirtualRepos(sha1, localOrRemoteRepo string) {
	virtualRepos, exists := gb.repoToVirtualMapping[localOrRemoteRepo]
	if !exists {
		gb.graphCreateRelationshipBinaryToRepo(sha1, localOrRemoteRepo)
	} else {
		for virtualRepo := range virtualRepos {
			gb.graphCreateRelationshipBinaryToRepo(sha1, virtualRepo)
		}
	}
}

func (gb GraphBuilder) graphAddCommand(cmd string) {
	if _, ok := gb.cypherCommands[cmd]; !ok {
		gb.cypherCommands[cmd] = true
		gb.graphBuilderCommands.WriteString(cmd + "\n")
	}
}

func (gb GraphBuilder) graphCreateRelationshipBinaryToRepo(binarySha, repoName string) {
	gb.graphAddCommand(fmt.Sprintf(`MATCH (bin:Binary {sha1: "%s"}), (repo {name: "%s"}) MERGE (bin)-[r:STORED_IN]-(repo) RETURN bin.name, type(r), repo.name;`,
		binarySha, repoName))
}

func (gb GraphBuilder) graphCreateRelationshipDependencyToBuild(buildName, buildNumber, binarySha string) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (build:Build {name: "%s", number: "%s"}) return build;`, buildName, buildNumber))
	gb.graphAddCommand(fmt.Sprintf(`MERGE (bin:Binary {sha1: "%s"}) return bin;`, binarySha))
	gb.graphAddCommand(fmt.Sprintf(`MATCH (build:Build {name: "%s", number: "%s"}), (bin:Binary {sha1: "%s"}) MERGE (bin)-[r:DEPENDENCY_FOR]->(build) RETURN build.name, type(r), bin.name;`,
		buildName, buildNumber, binarySha))
}

func (gb GraphBuilder) graphCreateRelationshipBuildToArtifact(buildName, buildNumber, binarySha string) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (build:Build {name: "%s", buildNumber: "%s"}) return build;`, buildName, buildNumber))
	gb.graphAddCommand(fmt.Sprintf(`MERGE (bin:Binary {sha1: "%s"}) return bin;`, binarySha))
	gb.graphAddCommand(fmt.Sprintf(`MATCH (bin:Binary {sha1: "%s"}), (build:Build {name: "%s", buildNumber: "%s"}) MERGE (build)-[r:PRODUCE]->(bin) RETURN bin.name, type(r), build.name;`,
		binarySha, buildName, buildNumber))
}

func (gb GraphBuilder) graphCreateRelationshipVirtualToLocalOrRemote(name, repo string) {
	gb.graphAddCommand(fmt.Sprintf(`MATCH (repoV:RepoVIRTUAL {name: "%s"}), (repo {name: "%s"}) MERGE (repoV)-[r:LINKED_TO]-(repo) RETURN repoV.name, type(r), repo.name;`,
		name, repo))
}

func (gb GraphBuilder) graphCreateRepoNode(name, repoType string, isPriority, isInc, isExc, isXray bool) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (repo:Repo%s {name: "%s", type: "%s", is_priority: "%s", is_inc: "%s", is_exc: "%s", is_xray: "%s"}) return repo;`,
		repoType, name, repoType, strconv.FormatBool(isPriority), strconv.FormatBool(isInc), strconv.FormatBool(isExc), strconv.FormatBool(isXray)))
}
