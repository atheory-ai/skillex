import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { mkdtemp, writeFile, readFile, rm, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";
const require = createRequire(import.meta.url);
const { target, verify, acquire, prepareBinary, TARGETS } = require("../npm/skillex/bin/acquire.js");
const { version } = require("../npm/skillex/package.json");
const sum = (data, name) => `${createHash("sha256").update(data).digest("hex")}  ${name}\n`;
async function temporary(fn) {
  const dir = await mkdtemp(path.join(tmpdir(), "skillex-wrapper-test-"));
  try { await fn(dir); } finally { await rm(dir, { recursive: true, force: true }); }
}

test("target matrix maps npm targets to version-pinned canonical archive names", () => {
  assert.equal(Object.keys(TARGETS).length, 5);
  assert.equal(target("linux", "x64").archive, `skillex_${version}_linux_amd64.tar.gz`);
  assert.equal(target("win32", "x64").archive, `skillex_${version}_windows_amd64.zip`);
  for (const platform of ["darwin", "linux"]) for (const arch of ["arm64", "x64"]) assert.match(target(platform, arch).archive, /\.tar\.gz$/);
  assert.throws(() => target("win32", "arm64"), /unsupported platform.*win32-arm64/);
});

test("checksum requires exact, unique entry and matching bytes", () => {
  const bytes = Buffer.from("archive");
  const checksum = sum(bytes, "release.tar.gz");
  verify(bytes, checksum, "release.tar.gz");
  assert.throws(() => verify(Buffer.from("tampered"), checksum, "release.tar.gz"), /mismatch/);
  assert.throws(() => verify(bytes, checksum, "other.tar.gz"), /exactly one/);
  assert.throws(() => verify(bytes, checksum + checksum, "release.tar.gz"), /exactly one/);
});

test("acquisition pins URLs, verifies before caching, and reuses cache offline", () => temporary(async (cacheRoot) => {
  const selected = target("linux", "x64");
  const bytes = Buffer.from("canonical archive");
  const urls = [];
  const download = async (url) => { urls.push(url); return url.endsWith("checksums.txt") ? Buffer.from(sum(bytes, selected.archive)) : bytes; };
  await acquire({ cacheRoot, target: selected, download });
  assert.deepEqual(urls.sort(), [`https://github.com/atheory-ai/skillex/releases/download/v${version}/${selected.archive}`, `https://github.com/atheory-ai/skillex/releases/download/v${version}/checksums.txt`].sort());
  const cached = await acquire({ cacheRoot, target: selected, offline: true, download: () => assert.fail("offline download") });
  assert.deepEqual(cached.archive, bytes);
  await writeFile(path.join(cacheRoot, version, selected.key, selected.archive), "tampered");
  await assert.rejects(acquire({ cacheRoot, target: selected, offline: true }), /checksum mismatch/);
  await acquire({ cacheRoot, target: selected, download });
  assert.deepEqual((await acquire({ cacheRoot, target: selected, offline: true })).archive, bytes);
}));

test("missing offline cache and corrupt network payload never execute", () => temporary(async (cacheRoot) => {
  const selected = target("linux", "x64");
  await assert.rejects(prepareBinary({ cacheRoot, offline: true }), /SKILLEX_OFFLINE=1 prevents downloads/);
  await assert.rejects(prepareBinary({ cacheRoot, target: selected, download: async (url) => url.endsWith("checksums.txt") ? Buffer.from(sum(Buffer.from("valid"), selected.archive)) : Buffer.from("corrupt") }), /checksum mismatch/);
}));

test("verified canonical tar archive executes with argument and exit code passthrough", { skip: process.platform === "win32" }, () => temporary(async (dir) => {
  const selected = target();
  const source = path.join(dir, "source");
  await mkdir(source);
  await writeFile(path.join(source, "skillex"), '#!/bin/sh\nprintf "%s\\n" "$@"\nexit 7\n');
  const archivePath = path.join(dir, selected.archive);
  execFileSync("tar", ["-czf", archivePath, "-C", source, "skillex"]);
  const bytes = await readFile(archivePath);
  const cache = path.join(dir, "cache", version, selected.key);
  await mkdir(cache, { recursive: true });
  await writeFile(path.join(cache, selected.archive), bytes);
  await writeFile(path.join(cache, "checksums.txt"), sum(bytes, selected.archive));
  const result = spawnSync(process.execPath, ["npm/skillex/bin/skillex.js", "hello world", "--version"], { env: { ...process.env, SKILLEX_CACHE_DIR: path.join(dir, "cache"), SKILLEX_OFFLINE: "1" }, encoding: "utf8" });
  assert.equal(result.status, 7, result.stderr);
  assert.equal(result.stdout, "hello world\n--version\n");
}));

test("packed package contains wrapper only and installs with lifecycle scripts disabled", () => temporary(async (dir) => {
  const result = JSON.parse(execFileSync("npm", ["pack", "./npm/skillex", "--json", "--pack-destination", dir], { encoding: "utf8", env: { ...process.env, npm_config_cache: path.join(dir, "npm-cache") } }));
  assert.deepEqual(result[0].files.map((file) => file.path).sort(), ["README.md", "bin/acquire.js", "bin/install.js", "bin/skillex.js", "package.json"]);
  execFileSync("npm", ["install", path.join(dir, result[0].filename), "--ignore-scripts", "--no-audit", "--no-fund", "--prefix", path.join(dir, "project")], { env: { ...process.env, npm_config_cache: path.join(dir, "npm-cache") }, stdio: "pipe" });
  const pkg = JSON.parse(await readFile(path.join(dir, "project/node_modules/@atheory-ai/skillex/package.json"), "utf8"));
  assert.equal(pkg.optionalDependencies, undefined);
  assert.equal(pkg.scripts.postinstall, "node bin/install.js");
}));

test("npm packaging and publication cannot gate canonical GitHub release", async () => {
  const workflow = await readFile(".github/workflows/release.yml", "utf8");
  const verification = workflow.split("  verify_release:")[1].split("  publish_release:")[0];
  assert.doesNotMatch(verification, /make npm-pack|Sync npm|Upload npm/);
  assert.match(workflow, /publish_release:\n\s+runs-on: ubuntu-latest\n\s+needs: verify_release/);
  assert.match(workflow, /publish_npm:\n\s+runs-on: ubuntu-latest\n\s+needs: \[verify_release, publish_release\]/);
  assert.doesNotMatch(workflow, /Publish platform|skillex-darwin-arm64-.*\.tgz/);
});

test("Windows canonical ZIP extraction selects skillex.exe", { skip: process.platform !== "darwin" }, () => temporary(async (dir) => {
  const selected = target("win32", "x64");
  await writeFile(path.join(dir, "skillex.exe"), "windows binary fixture");
  execFileSync("zip", ["-q", selected.archive, "skillex.exe"], { cwd: dir });
  const archive = await readFile(path.join(dir, selected.archive));
  const prepared = await prepareBinary({ cacheRoot: path.join(dir, "cache"), target: selected, download: async (url) => url.endsWith("checksums.txt") ? Buffer.from(sum(archive, selected.archive)) : archive });
  try { assert.equal(await readFile(prepared.binary, "utf8"), "windows binary fixture"); }
  finally { await prepared.cleanup(); }
}));

test("postinstall supports explicit skip and offline deferral", () => temporary(async (dir) => {
  for (const setting of [{ SKILLEX_SKIP_DOWNLOAD: "1" }, { SKILLEX_OFFLINE: "1" }]) {
    const result = spawnSync(process.execPath, ["npm/skillex/bin/install.js"], { env: { ...process.env, SKILLEX_CACHE_DIR: dir, ...setting }, encoding: "utf8" });
    assert.equal(result.status, 0);
    if (setting.SKILLEX_OFFLINE) assert.match(result.stderr, /retry acquisition on first use/);
    else assert.equal(result.stderr, "");
  }
}));
