import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { resolveApiBaseUrl } from "./config";

describe("API origin", () => {
  it("uses one local default for development and the secure page origin in production", () => {
    assert.equal(resolveApiBaseUrl(undefined), "http://localhost:8080");
    assert.equal(resolveApiBaseUrl(undefined, true, "https://demo.example"), "https://demo.example");
  });
  it("normalizes a configured origin", () => {
    assert.equal(resolveApiBaseUrl("https://api.example/", true), "https://api.example");
  });
  it("rejects insecure or secret-bearing production URLs", () => {
    assert.throws(() => resolveApiBaseUrl("http://api.example", true));
    assert.throws(() => resolveApiBaseUrl("https://user:secret@api.example", true));
    assert.throws(() => resolveApiBaseUrl("https://api.example/path", true));
  });
});
