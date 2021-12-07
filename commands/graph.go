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
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func GetGraphCommand() components.Command {
	return components.Command{
		Name:        "graph",
		Description: "Create a graph.",
		Aliases:     []string{"g"},
		Arguments:   getGraphArguments(),
		Flags:       getGraphFlags(),
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
	config, err := getGraphBuilderConfig(c)
	if err != nil {
		return err
	}
	graphBuilder := &GraphBuilder{
		builderConfig:        config,
		rtDetails:            rtDetails,
		graphBuilderCommands: []string{},
		cypherCommands:       make(map[string]bool),
		repoToVirtualMapping: make(map[string]map[string]bool),
		allRepos:             make(map[string]*CommonRepositoryDetails),
	}
	graphBuilder.serviceManager, err = utils.CreateServiceManager(graphBuilder.rtDetails, -1, false)
	if err != nil {
		return err
	}
	serviceDetails := graphBuilder.serviceManager.GetConfig().GetServiceDetails()
	graphBuilder.clientDetails = serviceDetails.CreateHttpClientDetails()
	graphBuilder.baseUrl = serviceDetails.GetUrl()
	graphBuilder.graphAddCommand("MERGE (x:Attacker {name:\"attacker\"});")
	return graphBuilder.makeGraph()
}

type GraphBuilder struct {
	baseUrl              string
	graphBuilderCommands []string
	cypherCommands       map[string]bool
	builderConfig        *graphBuilderConfig
	rtDetails            *config.ServerDetails
	repoToVirtualMapping map[string]map[string]bool
	clientDetails        httputils.HttpClientDetails
	serviceManager       artifactory.ArtifactoryServicesManager
	allRepos             map[string]*CommonRepositoryDetails
}

func getGraphBuilderConfig(c *components.Context) (*graphBuilderConfig, error) {
	graphUrl := c.GetStringFlagValue("graph-url")
	graphUser := c.GetStringFlagValue("graph-user")
	graphPassword := c.GetStringFlagValue("graph-password")
	graphDatabase := c.GetStringFlagValue("graph-database")
	if graphUrl != "" || graphUser != "" || graphPassword != "" || graphDatabase != "" {
		if graphUrl == "" || graphUser == "" || graphPassword == "" || graphDatabase == "" {
			return nil, errors.New("partial neo4j connection details provided, either provide none or the following: 'graph-url', 'graph-username', 'graph-password', 'graph-database'")
		}
	}
	graphRealm := c.GetStringFlagValue("graph-realm")
	verbose := c.GetBoolFlagValue("verbose")
	outToFile := c.GetBoolFlagValue("output-to-file")
	outFilePath := c.GetStringFlagValue("output-file-path")
	return &graphBuilderConfig{
		verbose:       verbose,
		graphUrl:      graphUrl,
		outToFile:     outToFile,
		graphUser:     graphUser,
		graphRealm:    graphRealm,
		outFilePath:   outFilePath,
		graphDatabase: graphDatabase,
		graphPassword: graphPassword,
	}, nil
}

type graphBuilderConfig struct {
	verbose       bool
	outToFile     bool
	graphUrl      string
	graphUser     string
	graphPassword string
	graphRealm    string
	outFilePath   string
	graphDatabase string
}

func (gb *GraphBuilder) makeGraph() error {
	startTime := time.Now()

	// Create repositories relations.
	err := gb.createRepositoriesGraphRelations()
	if err != nil {
		return err
	}
	// Create build relations.
	err = gb.createBuildsGraphRelations()
	if err != nil {
		return err
	}
	// Populate graph.
	err = gb.populateGraphDb()
	if err != nil {
		log.Error("Failed connecting to graphDB: " + err.Error())
	}
	// Output results.
	err = gb.outputResults()
	if err != nil {
		return err
	}
	endTime := time.Now()
	log.Info(fmt.Sprintf("Graph creation took: %f seconds", endTime.Sub(startTime).Seconds()))

	return nil
}

func (gb *GraphBuilder) outputResults() error {
	if gb.builderConfig.outToFile {
		fileName := gb.builderConfig.outFilePath
		if fileName == "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			fileName = "stechhelm-output-" + timestamp
		}
		err := ioutil.WriteFile(fileName, []byte(strings.Join(gb.graphBuilderCommands, "\n")), 0644)
		if err != nil {
			return errors.New("Failed creating file for output: " + err.Error())
		}
	}
	if gb.builderConfig.verbose {
		log.Info(fmt.Sprintf("Graph commands:\n%v", strings.Join(gb.graphBuilderCommands, "\n")))
	}
	return nil
}

func (gb *GraphBuilder) createBuildsGraphRelations() error {
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
	return nil
}

func (gb *GraphBuilder) handleBuilds(builds []Build) error {
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
			// Handle artifacts.
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

func (gb *GraphBuilder) handleArtifact(artifact *buildinfo.Artifact, buildInfo *buildinfo.BuildInfo,
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
		gb.linkBinToRepos(artifact.Sha1, repoName)
	}
	return nil
}

func (gb *GraphBuilder) handleDependency(dependency *buildinfo.Dependency, buildInfo *buildinfo.BuildInfo,
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
		gb.linkBinToRepos(dependency.Sha1, repoName)
	}
	return nil
}

func (gb *GraphBuilder) getRepositoryListBySha1(sha1 string) ([]Result, error) {
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

func (gb *GraphBuilder) getAllBuilds() ([]Build, error) {
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

func (gb *GraphBuilder) createRepositoriesGraphRelations() error {
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

func (gb *GraphBuilder) handleVirtualRepositories() error {
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
		isSafe := checkVirtualRepoSafety(&repositoryConfig, gb.allRepos)
		gb.graphCreateVirtualRepoNode(repositoryConfig.Key, "VIRTUAL", repositoryConfig.PriorityResolution,
			repositoryConfig.IncludesPattern != "**/*", repositoryConfig.ExcludesPattern != "", repositoryConfig.XrayIndex, isSafe)

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

func (gb *GraphBuilder) handleLocalRepositories() error {
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
		gb.allRepos[repositoryConfig.Key] = &repositoryConfig
	}
	return nil
}

func (gb *GraphBuilder) handleRemoteRepositories() error {
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
		gb.allRepos[repositoryConfig.Key] = &repositoryConfig
	}
	return nil
}

func (gb *GraphBuilder) linkBinToRepos(sha1, localOrRemoteRepo string) {
	localOrRemoteRepo = strings.TrimSuffix(localOrRemoteRepo, "-cache")
	repoConfig, ok := gb.allRepos[localOrRemoteRepo]
	if !ok {
		// Repo not found.
		return
	}
	if strings.EqualFold(repoConfig.Rclass, "local") {
		// Link to local.
		gb.graphCreateRelationshipBinaryToRepo(sha1, localOrRemoteRepo)
		return
	}
	// Link to virtual.
	virtualRepos, exists := gb.repoToVirtualMapping[localOrRemoteRepo]
	if !exists {
		gb.graphCreateRelationshipBinaryToRepo(sha1, localOrRemoteRepo)
	} else {
		for virtualRepo := range virtualRepos {
			gb.graphCreateRelationshipBinaryToRepo(sha1, virtualRepo)
		}
	}
}

func (gb *GraphBuilder) populateGraphDb() error {
	if gb.builderConfig.graphUrl == "" {
		return nil
	}
	driver, err := neo4j.NewDriver(gb.builderConfig.graphUrl, neo4j.BasicAuth(gb.builderConfig.graphUser, gb.builderConfig.graphPassword, gb.builderConfig.graphRealm))
	if err != nil {
		return err
	}
	defer func() { err = closeDbConnection(driver, err) }()
	session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead, DatabaseName: gb.builderConfig.graphDatabase})
	defer func() { err = closeDbConnection(session, err) }()
	log.Info("Populating graph data to neo4j, this may take a while...")
	for _, command := range gb.graphBuilderCommands {
		_, err = session.WriteTransaction(func(transaction neo4j.Transaction) (interface{}, error) {
			_, err := transaction.Run(command, nil)
			if err != nil {
				log.Error(fmt.Sprintf("Failed publishing command: %s to graphDB: %s", command, err.Error()))
			}
			return nil, err
		})
	}
	return err
}

func closeDbConnection(closer io.Closer, previousError error) error {
	err := closer.Close()
	if err == nil {
		return previousError
	}
	if previousError == nil {
		return err
	}
	return fmt.Errorf("%v closure error occurred:\n%s\ninitial error was:\n%w", reflect.TypeOf(closer), err.Error(), previousError)
}

func (gb *GraphBuilder) graphAddCommand(cmd string) {
	if _, ok := gb.cypherCommands[cmd]; !ok {
		gb.cypherCommands[cmd] = true
		gb.graphBuilderCommands = append(gb.graphBuilderCommands, cmd)
	}
}

func (gb *GraphBuilder) graphCreateRelationshipBinaryToRepo(binarySha, repoName string) {
	gb.graphAddCommand(fmt.Sprintf(`MATCH (bin:Binary {sha1: "%s"}), (repo {name: "%s"}) MERGE (repo)-[r:STORES]->(bin);`,
		binarySha, repoName))
}

func (gb *GraphBuilder) graphCreateRelationshipDependencyToBuild(buildName, buildNumber, binarySha string) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (build:Build {name: "%s", number: "%s"});`, buildName, buildNumber))
	gb.graphAddCommand(fmt.Sprintf(`MERGE (bin:Binary {sha1: "%s"});`, binarySha))
	gb.graphAddCommand(fmt.Sprintf(`MATCH (build:Build {name: "%s", number: "%s"}), (bin:Binary {sha1: "%s"}) MERGE (bin)-[r:DEPENDENCY_FOR]->(build);`,
		buildName, buildNumber, binarySha))
}

func (gb *GraphBuilder) graphCreateRelationshipBuildToArtifact(buildName, buildNumber, binarySha string) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (build:Build {name: "%s", number: "%s"});`, buildName, buildNumber))
	gb.graphAddCommand(fmt.Sprintf(`MERGE (bin:Binary {sha1: "%s"});`, binarySha))
	gb.graphAddCommand(fmt.Sprintf(`MATCH (bin:Binary {sha1: "%s"}), (build:Build {name: "%s", number: "%s"}) MERGE (build)-[r:PRODUCE]->(bin);`,
		binarySha, buildName, buildNumber))
}

func (gb *GraphBuilder) graphCreateRelationshipVirtualToLocalOrRemote(name, repo string) {
	gb.graphAddCommand(fmt.Sprintf(`MATCH (repoV:RepoVIRTUAL {name: "%s"}), (repo {name: "%s"}) MERGE (repo)-[r:LINKED_TO]->(repoV);`,
		name, repo))
}

func (gb *GraphBuilder) graphCreateVirtualRepoNode(name, repoType string, isPriority, isInc, isExc, isXray, isSafe bool) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (repo:Repo%s {name: "%s", type: "%s", is_priority: "%s", is_inc: "%s", is_exc: "%s", is_xray: "%s", is_safe: "%s"});`,
		repoType, name, repoType, strconv.FormatBool(isPriority), strconv.FormatBool(isInc), strconv.FormatBool(isExc), strconv.FormatBool(isXray), strconv.FormatBool(isSafe)))
}

func (gb *GraphBuilder) graphCreateRepoNode(name, repoType string, isPriority, isInc, isExc, isXray bool) {
	gb.graphAddCommand(fmt.Sprintf(`MERGE (repo:Repo%s {name: "%s", type: "%s", is_priority: "%s", is_inc: "%s", is_exc: "%s", is_xray: "%s"});`,
		repoType, name, repoType, strconv.FormatBool(isPriority), strconv.FormatBool(isInc), strconv.FormatBool(isExc), strconv.FormatBool(isXray)))
	if strings.EqualFold("remote", repoType) {
		gb.graphAddCommand(fmt.Sprintf(`MATCH (x:Attacker {name:"attacker"}), (repo:RepoREMOTE {name: "%s"}) MERGE (x)-[r:ATTACKS]->(repo);`, name))
	}
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

func getGraphArguments() []components.Argument {
	return []components.Argument{}
}

func getGraphFlags() []components.Flag {
	return []components.Flag{
		components.StringFlag{
			Name:        "server-id",
			Description: "Artifactory server ID configured using the config command.",
		},
		components.BoolFlag{
			Name:         "verbose",
			Description:  "[Default: false] Set to true to output the graph-building queries to stdout.",
			DefaultValue: false,
		},
		components.StringFlag{
			Name:        "graph-url",
			Description: "neo4j URL.",
		},
		components.StringFlag{
			Name:        "graph-user",
			Description: "neo4j username.",
		},
		components.StringFlag{
			Name:        "graph-password",
			Description: "neo4j password.",
		},
		components.StringFlag{
			Name:        "graph-database",
			Description: "neo4j database name.",
		},
		components.StringFlag{
			Name:        "graph-realm",
			Description: "neo4j realm.",
		},
		components.BoolFlag{
			Name:         "output-to-file",
			Description:  "[Default: false] Set to true to output the graph-building queries to a file.",
			DefaultValue: false,
		},
		components.StringFlag{
			Name:        "output-file-path",
			Description: "[Default: current workdir] Path to an output file for the graph-building queries.",
		},
	}
}
