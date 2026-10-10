import { createCipheriv } from "node:crypto";
import { createServer } from "node:http";
import { pathToFileURL } from "node:url";

const AES_KEY_LENGTHS = new Set([16, 24, 32]);
const MOBILE_PATTERN = /^\+861[3-9]\d{9}$/;
const CODE_PATTERN = /^\d{4,8}$/;

function required(env, name) {
  const value = env[name];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${name} is required`);
  }
  return value;
}

function parseInteger(name, value, { min, max = Number.MAX_SAFE_INTEGER }) {
  if (!/^\d+$/.test(value)) {
    throw new Error(`${name} must be an integer between ${min} and ${max}`);
  }

  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < min || parsed > max) {
    throw new Error(`${name} must be an integer between ${min} and ${max}`);
  }
  return parsed;
}

export function loadConfig(env = process.env) {
  const token = required(env, "SMS_BRIDGE_TOKEN");
  if (token.length < 32) {
    throw new Error("SMS_BRIDGE_TOKEN must contain at least 32 characters");
  }

  const upstreamUrl = required(env, "SMS_UPSTREAM_URL");
  let parsedUpstreamUrl;
  try {
    parsedUpstreamUrl = new URL(upstreamUrl);
  } catch {
    throw new Error("SMS_UPSTREAM_URL must be an HTTP(S) URL");
  }
  if (parsedUpstreamUrl.protocol !== "http:" && parsedUpstreamUrl.protocol !== "https:") {
    throw new Error("SMS_UPSTREAM_URL must be an HTTP(S) URL");
  }

  const mobileAesKey = required(env, "SMS_MOBILE_AES_KEY");
  if (!AES_KEY_LENGTHS.has(Buffer.byteLength(mobileAesKey, "utf8"))) {
    throw new Error("SMS_MOBILE_AES_KEY must be 16, 24, or 32 UTF-8 bytes");
  }

  const codeParam = env.SMS_CODE_PARAM ?? "code";
  if (typeof codeParam !== "string" || codeParam.trim() === "") {
    throw new Error("SMS_CODE_PARAM must not be empty");
  }

  return {
    port: parseInteger("SMS_BRIDGE_PORT", env.SMS_BRIDGE_PORT ?? "8080", {
      min: 1,
      max: 65535,
    }),
    token,
    upstreamUrl,
    businessCode: required(env, "SMS_BUSINESS_CODE"),
    messageEnumCode: required(env, "SMS_MESSAGE_ENUM_CODE"),
    codeParam,
    mobileAesKey,
    timeoutMs: parseInteger(
      "SMS_UPSTREAM_TIMEOUT_MS",
      env.SMS_UPSTREAM_TIMEOUT_MS ?? "5000",
      { min: 1 },
    ),
  };
}

export function encryptMobile(mobile, key) {
  const keyBuffer = Buffer.from(key, "utf8");
  if (!AES_KEY_LENGTHS.has(keyBuffer.length)) {
    throw new Error("AES key must be 16, 24, or 32 UTF-8 bytes");
  }

  const cipher = createCipheriv(`aes-${keyBuffer.length * 8}-ecb`, keyBuffer, null);
  return Buffer.concat([cipher.update(mobile, "utf8"), cipher.final()]).toString("base64");
}

function writeJson(response, statusCode, body) {
  response.writeHead(statusCode, { "content-type": "application/json" });
  response.end(JSON.stringify(body));
}

async function parseJsonBody(request) {
  let body = "";
  request.setEncoding("utf8");
  for await (const chunk of request) body += chunk;
  return JSON.parse(body);
}

export function createBridgeServer(config) {
  return createServer(async (request, response) => {
    if (request.method !== "POST" || request.url !== "/send") {
      writeJson(response, 404, { ok: false });
      return;
    }

    if (request.headers.authorization !== `Bearer ${config.token}`) {
      writeJson(response, 401, { ok: false });
      return;
    }

    let body;
    try {
      body = await parseJsonBody(request);
    } catch {
      writeJson(response, 400, { ok: false });
      return;
    }

    if (
      body === null
      || typeof body !== "object"
      || typeof body.mobile !== "string"
      || !MOBILE_PATTERN.test(body.mobile)
      || typeof body.code !== "string"
      || !CODE_PATTERN.test(body.code)
    ) {
      writeJson(response, 400, { ok: false });
      return;
    }

    const upstreamBody = {
      messageEnumCode: config.messageEnumCode,
      businessCode: config.businessCode,
      mobile: encryptMobile(body.mobile.slice(3), config.mobileAesKey),
      timestamp: Date.now(),
      param: { [config.codeParam]: body.code },
    };

    try {
      const upstreamResponse = await fetch(config.upstreamUrl, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(upstreamBody),
        signal: AbortSignal.timeout(config.timeoutMs),
      });

      if (!upstreamResponse.ok) {
        writeJson(response, 502, { ok: false });
        return;
      }

      const upstreamResult = await upstreamResponse.json();
      if (String(upstreamResult?.code) !== "200") {
        writeJson(response, 502, { ok: false });
        return;
      }

      writeJson(response, 200, { ok: true });
    } catch {
      writeJson(response, 502, { ok: false });
    }
  });
}

const isMain = process.argv[1]
  && import.meta.url === pathToFileURL(process.argv[1]).href;

if (isMain) {
  const config = loadConfig();
  createBridgeServer(config).listen(config.port, "0.0.0.0");
}
