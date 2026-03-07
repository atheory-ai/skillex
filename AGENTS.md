# AGENTS

This file documents how to work in this repository.


<!-- skillex:start -->
## Skillex

This project uses Skillex for skill management. Use the skillex MCP server
if available (preferred), otherwise use the CLI commands below.

### MCP (preferred)

If the `skillex` MCP server is connected, use it directly:

- Use the `skillex_query` tool with parameters: path, topic, tags, package, format.
- Browse available skills through MCP resource discovery.

### CLI (fallback)

If MCP is not available, query skills via the command line:

```
  skillex query --path <filepath>
  skillex query --topic <topic> --tags <tags>
  skillex query --package <package>
  skillex query --path <glob> --topic <topic> --format content
```

### Available scopes

  - **

### Available topics

  repo-conventions

### Available tags

  getting-started

<!-- skillex:end -->
