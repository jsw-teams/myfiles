(function () {
  const site = "https://files.js.gripe";

  const discoveryResources = [
    `${site}/robots.txt`,
    `${site}/llms.txt`,
    `${site}/llms-full.txt`,
    `${site}/sitemap.xml`,
    `${site}/auth.md`,
    `${site}/openapi.json`,
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
      description: "List public discovery resources for Files.js.gripe and explain public file-link access.",
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: false
      },
      execute: async () => ({
        site,
        resources: discoveryResources,
        crawlingPolicy: "Public /files and /files/raw links may be fetched directly. Authenticated pages, APIs, admin pages, setup, upload result pages, and pickup flows are not crawl targets. API clients should read /openapi.json instead of crawling /api/.",
        uploadProtocol: "Use the R2 direct-upload flow: POST /api/upload/r2/init, PUT to returned presigned R2 URL(s), then POST /api/upload/r2/complete. The legacy POST /api/upload endpoint is retired."
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
        publicFilePaths: ["/files/{id}.{ext}", "/files/raw/{id}.{ext}"],
        uploadApi: {
          current: ["/api/upload/r2/init", "presigned R2 PUT", "/api/upload/r2/complete"],
          cancel: "/api/upload/r2/cancel",
          retired: "/api/upload"
        },
        blockedPaths: ["/api/", "/admin", "/setup", "/dashboard/", "/uploads/", "/file/", "/f/", "/pickup/"],
        note: "Public /files and /files/raw links can be fetched directly while they remain public. Upload clients should follow /openapi.json and the R2 direct-upload sequence."
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
