(function () {
  const site = "https://files.js.gripe";

  const discoveryResources = [
    `${site}/robots.txt`,
    `${site}/llms.txt`,
    `${site}/llms-full.txt`,
    `${site}/sitemap.xml`,
    `${site}/auth.md`,
    `${site}/.well-known/api-catalog`,
    `${site}/.well-known/oauth-protected-resource`,
    `${site}/.well-known/oauth-authorization-server`,
    `${site}/.well-known/openid-configuration`,
    `${site}/.well-known/mcp/server-card.json`,
    `${site}/.well-known/agent-skills/index.json`
  ];

  const tools = [
    {
      name: "list_discovery_resources",
      description: "List public discovery resources for Files.js.gripe. This does not access uploaded files.",
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: false
      },
      execute: async () => ({
        site,
        resources: discoveryResources,
        crawlingPolicy: "Uploaded files and authenticated pages are not suitable for crawling, indexing, AI training, or agent input."
      })
    },
    {
      name: "describe_file_service_policy",
      description: "Describe Files.js.gripe crawler, AI-use, and authentication boundaries.",
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: false
      },
      execute: async () => ({
        site,
        protected: true,
        authentication: `${site}/auth.md`,
        openapi: `${site}/openapi.json`,
        oauthProtectedResource: `${site}/.well-known/oauth-protected-resource`,
        blockedPaths: ["/api/", "/admin", "/setup", "/dashboard/", "/uploads/", "/file/", "/f/"],
        note: "Do not crawl, train on, summarize, extract, or use uploaded files as AI input."
      })
    }
  ];

  const context = {
    name: "Files.js.gripe Discovery",
    description: "Read-only discovery context for the protected Files.js.gripe file service.",
    tools
  };

  try {
    if (navigator.modelContext?.provideContext) {
      navigator.modelContext.provideContext(context);
    }
    if (document.modelContext?.registerTool) {
      for (const tool of tools) {
        document.modelContext.registerTool(tool);
      }
    }
    window.myfilesAgentDiscovery = context;
  } catch (error) {
    window.myfilesAgentDiscoveryError = String(error?.message || error);
  }
})();
