import assert from "node:assert/strict";
import { createDecipheriv } from "node:crypto";
import { createServer } from "node:http";
import test from "node:test";

import { createBridgeServer, encryptMobile, loadConfig } from "./server.mjs";

const TOKEN = "0123456789abcdef0123456789abcdef";
const AES_KEY = "0123456789abcdef0123456789abcdef";

function testEnv(overrides = {}) {
  return {
    SMS_BRIDGE_PORT: "8080",
    SMS_BRIDGE_TOKEN: TOKEN,
    SMS_UPSTREAM_URL: "http://127.0.0.1:12345/NotifyService/sendMessage",
    SMS_BUSINESS_CODE: "VERIFY_BUSINESS",
    SMS_MESSAGE_ENUM_CODE: "SMS",
    SMS_CODE_PARAM: "code",
    SMS_MOBILE_AES_KEY: AES_KEY,
    SMS_UPSTREAM_TIMEOUT_MS: "5000",
    ...overrides,
  };
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve(server.address().port);
    });
  });
}

function close(server) {
  if (!server.listening) return Promise.resolve();
  return new Promise((resolve, reject) => {
    server.close((error) => (error ? reject(error) : resolve()));
  });
}

async function post(port, {
  token = TOKEN,
  body = { mobile: "+8613800138000", code: "123456" },
  path = "/send",
  method = "POST",
} = {}) {
  return fetch(`http://127.0.0.1:${port}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
    },
    body: method === "POST" ? JSON.stringify(body) : undefined,
  });
}

async function withUpstream(handler, run) {
  const requests = [];
  const upstream = createServer(async (request, response) => {
    let raw = "";
    for await (const chunk of request) raw += chunk;
    requests.push(JSON.parse(raw));
    await handler(request, response);
  });
  const upstreamPort = await listen(upstream);
  const config = loadConfig(testEnv({
    SMS_UPSTREAM_URL: `http://127.0.0.1:${upstreamPort}/NotifyService/sendMessage`,
  }));
  const bridge = createBridgeServer(config);
  const bridgePort = await listen(bridge);

  try {
    await run({ bridgePort, requests, config });
  } finally {
    await close(bridge);
    await close(upstream);
  }
}

test("loadConfig identifies a missing required setting", () => {
  const env = testEnv();
  delete env.SMS_BUSINESS_CODE;
  assert.throws(() => loadConfig(env), /SMS_BUSINESS_CODE/);
});

test("loadConfig rejects an unsupported AES key length", () => {
  assert.throws(
    () => loadConfig(testEnv({ SMS_MOBILE_AES_KEY: "123456789012345" })),
    /SMS_MOBILE_AES_KEY/,
  );
});

test("encryptMobile uses AES-256-ECB with automatic padding and Base64 output", () => {
  const encrypted = encryptMobile("13800138000", AES_KEY);
  const decipher = createDecipheriv("aes-256-ecb", Buffer.from(AES_KEY, "utf8"), null);
  const decrypted = Buffer.concat([
    decipher.update(Buffer.from(encrypted, "base64")),
    decipher.final(),
  ]).toString("utf8");

  assert.equal(decrypted, "13800138000");
});

test("a valid request is transformed and sent upstream exactly once", async () => {
  await withUpstream((_request, response) => {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ code: "200" }));
  }, async ({ bridgePort, requests }) => {
    const before = Date.now();
    const response = await post(bridgePort);
    const after = Date.now();

    assert.equal(response.status, 200);
    assert.deepEqual(await response.json(), { ok: true });
    assert.equal(requests.length, 1);
    assert.equal(requests[0].messageEnumCode, "SMS");
    assert.equal(requests[0].businessCode, "VERIFY_BUSINESS");
    assert.deepEqual(requests[0].param, { code: "123456" });
    assert.ok(requests[0].timestamp >= before && requests[0].timestamp <= after);

    const decipher = createDecipheriv("aes-256-ecb", Buffer.from(AES_KEY, "utf8"), null);
    const mobile = Buffer.concat([
      decipher.update(Buffer.from(requests[0].mobile, "base64")),
      decipher.final(),
    ]).toString("utf8");
    assert.equal(mobile, "13800138000");
  });
});

for (const scenario of [
  { name: "business failure", status: 200, payload: { code: 500 } },
  { name: "non-2xx response", status: 503, payload: { code: 200 } },
  { name: "invalid JSON", status: 200, payload: "not-json", raw: true },
]) {
  test(`an upstream ${scenario.name} returns 502`, async () => {
    await withUpstream((_request, response) => {
      response.writeHead(scenario.status, { "content-type": "application/json" });
      response.end(scenario.raw ? scenario.payload : JSON.stringify(scenario.payload));
    }, async ({ bridgePort }) => {
      const response = await post(bridgePort);
      assert.equal(response.status, 502);
      assert.deepEqual(await response.json(), { ok: false });
    });
  });
}

test("an upstream timeout returns 502", async () => {
  await withUpstream(async (_request, response) => {
    await new Promise((resolve) => setTimeout(resolve, 100));
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ code: 200 }));
  }, async ({ bridgePort, config }) => {
    config.timeoutMs = 50;
    const response = await post(bridgePort);
    assert.equal(response.status, 502);
  });
});

test("an upstream network error returns 502", async () => {
  const unusedServer = createServer();
  const unusedPort = await listen(unusedServer);
  await close(unusedServer);

  const config = loadConfig(testEnv({
    SMS_UPSTREAM_URL: `http://127.0.0.1:${unusedPort}/NotifyService/sendMessage`,
  }));
  const bridge = createBridgeServer(config);
  const bridgePort = await listen(bridge);
  try {
    const response = await post(bridgePort);
    assert.equal(response.status, 502);
    assert.deepEqual(await response.json(), { ok: false });
  } finally {
    await close(bridge);
  }
});

for (const scenario of [
  { name: "token", expected: 401, request: { token: "wrong" } },
  {
    name: "mobile",
    expected: 400,
    request: { body: { mobile: "13800138000", code: "123456" } },
  },
  {
    name: "code",
    expected: 400,
    request: { body: { mobile: "+8613800138000", code: "12ab56" } },
  },
]) {
  test(`an invalid ${scenario.name} is rejected without calling upstream`, async () => {
    await withUpstream((_request, response) => {
      response.end(JSON.stringify({ code: 200 }));
    }, async ({ bridgePort, requests }) => {
      const response = await post(bridgePort, scenario.request);
      assert.equal(response.status, scenario.expected);
      assert.equal(requests.length, 0);
    });
  });
}

test("invalid request JSON is rejected without calling upstream", async () => {
  await withUpstream((_request, response) => response.end(), async ({ bridgePort, requests }) => {
    const response = await fetch(`http://127.0.0.1:${bridgePort}/send`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${TOKEN}`,
        "content-type": "application/json",
      },
      body: "{",
    });
    assert.equal(response.status, 400);
    assert.equal(requests.length, 0);
  });
});

test("unsupported routes return 404", async () => {
  await withUpstream((_request, response) => response.end(), async ({ bridgePort, requests }) => {
    const response = await post(bridgePort, { path: "/health" });
    assert.equal(response.status, 404);
    assert.equal(requests.length, 0);
  });
});
