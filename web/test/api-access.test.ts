import { expect, test } from "bun:test";
import { apiClient, ApiError } from "../src/services/api/request";
import { createAPIKey, listAPIKeys, revokeAPIKey, apiKeyStatus } from "../src/services/api/api-keys";
import { apiDocumentationBaseURL, modelCallExample } from "../src/pages/api-docs/api-examples";

test("Key management uses the expected routes and preserves failed writes", async () => {
    const original = apiClient.request;
    const requests: Array<Record<string, unknown>> = [];
    apiClient.request = (async (config: Record<string, unknown>) => {
        requests.push(config);
        return { status: 200, data: { code: 0, data: { keys: [] }, msg: "ok" } };
    }) as typeof apiClient.request;
    try {
        const signal = new AbortController().signal;
        await listAPIKeys(signal);
        await createAPIKey({ name: "automation", expiresAt: "2027-01-01T00:00:00Z" });
        await revokeAPIKey("key/id");
        expect(requests[0]).toMatchObject({ method: "get", url: "/api-keys", signal });
        expect(requests[1]).toMatchObject({ method: "post", url: "/api-keys", data: { name: "automation", expiresAt: "2027-01-01T00:00:00Z" } });
        expect(requests[2]).toMatchObject({ method: "delete", url: "/api-keys/key%2Fid" });
        apiClient.request = (async () => ({ status: 403, data: { code: 403, data: null, msg: "forbidden", reason: "forbidden" } })) as typeof apiClient.request;
        await expect(revokeAPIKey("key")).rejects.toBeInstanceOf(ApiError);
    } finally {
        apiClient.request = original;
    }
});

test("Key state respects revocation and the expiry boundary", () => {
    const expiresAt = "2027-01-01T00:00:00Z";
    const boundary = Date.parse(expiresAt);
    expect(apiKeyStatus({ revokedAt: null, expiresAt: null }, boundary)).toBe("active");
    expect(apiKeyStatus({ revokedAt: null, expiresAt }, boundary - 1)).toBe("active");
    expect(apiKeyStatus({ revokedAt: null, expiresAt }, boundary)).toBe("expired");
    expect(apiKeyStatus({ revokedAt: expiresAt, expiresAt: null }, boundary)).toBe("revoked");
});

test("Documentation examples use the deployed API root and safely quote user model IDs", () => {
    expect(apiDocumentationBaseURL("/api", "https://canvas.example")).toBe("https://canvas.example/api");
    expect(apiDocumentationBaseURL("https://backend.example/base/", "https://canvas.example")).toBe("https://backend.example/base");
    const example = modelCallExample("https://canvas.example/api", "channel::O'Hara", "text");
    expect(example).toContain("https://canvas.example/api/model-calls");
    expect(example).toContain("$CANVAS_API_KEY");
    expect(example).toContain("O'\\''Hara");
    expect(example).toContain('"messages"');
    expect(example).toContain('"maxOutputTokens": 512');
    expect(modelCallExample("https://canvas.example/api", "image-model", "image")).toContain('"count": 1');
});

test.skipIf(process.platform === "win32")("Copied curl executes with intact flags and JSON without running shell content from the model", () => {
    const example = modelCallExample("https://canvas.example/api", "channel::O'Hara", "text");
    const result = Bun.spawnSync(["/bin/sh", "-c", `curl() { printf '%s\\0' "$@"; }; export CANVAS_API_KEY=example; ${example}`]);
    const args = new TextDecoder().decode(result.stdout).split("\0").filter(Boolean);
    expect(result.exitCode).toBe(0);
    expect(args).toHaveLength(8);
    expect(args[3]).toBe("Authorization: Bearer example");
    expect(JSON.parse(args[7]).model).toBe("channel::O'Hara");
});
