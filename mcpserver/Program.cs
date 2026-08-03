using System.Text.Json;
using System.IdentityModel.Tokens.Jwt;
using Microsoft.AspNetCore.Authentication.JwtBearer;
using Microsoft.IdentityModel.JsonWebTokens;
using Microsoft.IdentityModel.Tokens;
using ModelContextProtocol;
using ModelContextProtocol.AspNetCore;
using ModelContextProtocol.AspNetCore.Authentication;
using ModelContextProtocol.Protocol;
using TaskWizard.McpServer.Services;

var builder = WebApplication.CreateBuilder(args);

var tenantId = Environment.GetEnvironmentVariable("TW_ENTRA_TENANT_ID") ?? "";
var audience = Environment.GetEnvironmentVariable("TW_ENTRA_AUDIENCE") ?? "";
var clientId = Environment.GetEnvironmentVariable("TW_ENTRA_CLIENT_ID") ?? "";
var mcpResource = Environment.GetEnvironmentVariable("TW_MCP_RESOURCE") ?? "";
var configuredIssuer = Environment.GetEnvironmentVariable("TW_ENTRA_ISSUER")?.Trim();
if (string.IsNullOrWhiteSpace(configuredIssuer))
    configuredIssuer = null;

if (string.IsNullOrWhiteSpace(audience))
    throw new InvalidOperationException("TW_ENTRA_AUDIENCE must be set to a valid Entra audience.");

if (string.IsNullOrWhiteSpace(clientId))
    throw new InvalidOperationException("TW_ENTRA_CLIENT_ID must be set to the Entra app registration's client ID.");

if (string.IsNullOrWhiteSpace(mcpResource))
    throw new InvalidOperationException("TW_MCP_RESOURCE must be set to the canonical URL of this MCP server (e.g. https://mcp.example.com).");

var multiTenant = string.IsNullOrWhiteSpace(tenantId) && string.IsNullOrWhiteSpace(configuredIssuer);
var authorityTenant = string.IsNullOrWhiteSpace(tenantId) ? "common" : tenantId.Trim();
var authority = configuredIssuer
    ?? $"https://login.microsoftonline.com/{authorityTenant}/v2.0";
var apiUrl = Environment.GetEnvironmentVariable("TW_API_URL") ?? "http://localhost:2021";

builder.Services.AddHttpContextAccessor();

builder.Services.AddHttpClient("ApiServer", client =>
{
    client.BaseAddress = new Uri(apiUrl.TrimEnd('/') + "/");
});

builder.Services.AddScoped<ApiProxyService>();

builder.Services.AddAuthentication(options =>
{
    options.DefaultAuthenticateScheme = JwtBearerDefaults.AuthenticationScheme;
    options.DefaultChallengeScheme = McpAuthenticationDefaults.AuthenticationScheme;
})
.AddJwtBearer(options =>
{
    options.Authority = authority;
    options.TokenValidationParameters = new TokenValidationParameters
    {
        ValidateIssuer = true,
        ValidateAudience = true,
        ValidateLifetime = true,
        ValidateIssuerSigningKey = true,
        ValidAudiences = new[] { audience, clientId },
        IssuerValidator = (issuer, securityToken, _) => ValidateIssuer(issuer, securityToken, authority, multiTenant),
    };
})
.AddMcp(options =>
{
    options.ResourceMetadata = new()
    {
        Resource = mcpResource,
        AuthorizationServers = { authority },
        ScopesSupported = {
            $"{audience}/User.Read",
            $"{audience}/Labels.Read",
            $"{audience}/Labels.Write",
            $"{audience}/Tasks.Read",
            $"{audience}/Tasks.Write",
        },
        BearerMethodsSupported = { "header" },
    };
});

builder.Services.AddAuthorization();

builder.Services
    .AddMcpServer()
    .WithHttpTransport()
    .WithToolsFromAssembly()
    .WithRequestFilters(filters => filters.AddCallToolFilter(next => async (context, cancellationToken) =>
    {
        try
        {
            return await next(context, cancellationToken);
        }
        catch (OperationCanceledException)
        {
            throw;
        }
        catch (McpException)
        {
            // Already surfaced with a descriptive message by the SDK.
            throw;
        }
        catch (JsonException ex)
        {
            var toolName = context.Params?.Name ?? "<unknown>";
            var message =
                $"Invalid arguments for tool '{toolName}': {ex.Message} " +
                "Check that each argument matches the declared type in the tool schema " +
                "(for example, booleans must be sent as true/false, not as quoted strings).";
            return new CallToolResult
            {
                IsError = true,
                Content = [new TextContentBlock { Text = message }],
            };
        }
        catch (Exception ex)
        {
            var toolName = context.Params?.Name ?? "<unknown>";
            var message = $"Tool '{toolName}' failed: {ex.GetType().Name}: {ex.Message}";
            return new CallToolResult
            {
                IsError = true,
                Content = [new TextContentBlock { Text = message }],
            };
        }
    }));

var app = builder.Build();

if (!app.Environment.IsDevelopment())
{
    app.UseHsts();
    app.UseHttpsRedirection();
}

app.UseAuthentication();
app.UseAuthorization();
app.MapMcp().RequireAuthorization();

app.Run();

static string ValidateIssuer(string issuer, SecurityToken securityToken, string authority, bool multiTenant)
{
    if (!multiTenant)
    {
        if (issuer == authority)
            return issuer;

        throw new SecurityTokenInvalidIssuerException("Invalid token issuer.");
    }

    var tenantId = securityToken switch
    {
        JwtSecurityToken jwt => jwt.Claims.FirstOrDefault(claim => claim.Type == "tid")?.Value,
        JsonWebToken jsonWebToken => jsonWebToken.GetClaim("tid")?.Value,
        _ => null,
    };
    var expectedIssuer = $"https://login.microsoftonline.com/{tenantId}/v2.0";
    if (string.IsNullOrWhiteSpace(tenantId) || issuer != expectedIssuer)
        throw new SecurityTokenInvalidIssuerException("Invalid token issuer.");

    return issuer;
}
