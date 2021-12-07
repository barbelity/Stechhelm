# stechhelm

## About this plugin
This plugin was created to analyse and visualise potential threats and risks in JFrog platforms.
It is done by understanding the relations between JFrog components, and building a corresponding graph in neo4j.

## Installation with JFrog CLI
Installing the latest version:

`$ jfrog plugin install stechhelm`

Installing a specific version:

`$ jfrog plugin install stechhelm@version`

Uninstalling a plugin

`$ jfrog plugin uninstall stechhelm`

## Usage
### Commands
* audit
    - Flags:
        - --server-id: Artifactory server ID configured using the config command **[Optional]**
    - Example:
    ```
      $ jfrog stechhelm audit
    ```
* graph
    - Flags:
        - --server-id: Artifactory server ID configured using the config command **[Optional]**
        - --verbose: Set to true to output the graph-building queries to stdout. **[Optional]**
        - --graph-url: neo4j URL.
        - --graph-user: neo4j username.
        - --graph-password: neo4j password.
        - --graph-database: neo4j database name.
        - --graph-realm: neo4j realm. **[Optional]**
        - --output-to-file: [Default: false] Set to true to output the graph-building queries to a file.
        - --output-file-path: [Default: current workdir] Path to an output file for the graph-building queries. **[Optional]**
    - Example:
  ```
    $ jfrog stechhelm graph --graph-url="http://url.com:8080/" --graph-user=user --graph-password=pass --graph-database=default
  ```

## Additional info
Here are some useful queries to use in neo4j, after creating the graph:

* Show the whole graph:
    ```
        MATCH (n1)-[r]->(n2) RETURN r, n1, n2
    ```
* Find the shortest path - from an attacker to each vulnerable build
    ```
        MATCH p = shortestPath((x:RepoVIRTUAL)-[r2:STORES|PRODUCE|DEPENDENCY_FOR*1..10]->(b:Build)),(n)-[r3:LINKED_TO|ATTACKS*1..4]->(x)
        WHERE x.is_safe = "false"
        RETURN *
    ```

## Release Notes
The release notes are available [here](RELEASE.md).
