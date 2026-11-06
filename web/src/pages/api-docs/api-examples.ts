export type APICapability = "text" | "image" | "video" | "audio";

export function apiDocumentationBaseURL(base: string, origin: string) {
    return new URL(base, origin).href.replace(/\/$/, "");
}

function shellQuote(value: string) {
    return `'${value.replace(/'/g, "'\\''")}'`;
}

export function modelCallExample(apiRoot: string, model: string, capability: APICapability) {
    const request =
        capability === "text"
            ? {
                  model,
                  messages: [
                      { role: "system", content: "请简洁回答。" },
                      { role: "user", content: "用一句话介绍影策。" },
                  ],
                  parameters: { maxOutputTokens: 512 },
              }
            : {
                  capability,
                  model,
                  prompt: capability === "audio" ? "你好，欢迎来到影策。" : "雨夜的城市街道，电影质感",
                  parameters: capability === "image" ? { aspectRatio: "1:1", count: 1 } : capability === "video" ? { duration: 5 } : { audioFormat: "mp3" },
              };
    return `curl -sS ${shellQuote(`${apiRoot}/model-calls`)} \\\n  -H "Authorization: Bearer $CANVAS_API_KEY" \\\n  -H 'Content-Type: application/json' \\\n  -d ${shellQuote(JSON.stringify(request, null, 2))}`;
}
