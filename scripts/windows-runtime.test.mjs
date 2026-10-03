import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { mkdtemp, mkdir, copyFile, cp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const { target } = require("../npm/skillex/bin/acquire.js");
const { version } = require("../npm/skillex/package.json");

test("Windows ZIP wrapper executes and generated npm/pnpm/source commands run", { skip: process.platform !== "win32" }, async () => {
  const dir = await mkdtemp(path.join(tmpdir(), "skillex windows runtime "));
  const binary = path.resolve(".skillex/bin/skillex.exe");
  const selected = target();
  const archive = path.join(dir, selected.archive);
  const zipSource = path.join(dir, "zip source");
  await mkdir(zipSource);
  await copyFile(binary, path.join(zipSource, "skillex.exe"));
  try {
    execFileSync("pwsh", ["-NoProfile", "-Command", "Compress-Archive -LiteralPath $env:SKILLEX_ZIP_SOURCE -DestinationPath $env:SKILLEX_ZIP_OUTPUT"], {
      env: { ...process.env, SKILLEX_ZIP_SOURCE: path.join(zipSource, "skillex.exe"), SKILLEX_ZIP_OUTPUT: archive },
    });
    const bytes = await readFile(archive);
    const cacheRoot = path.join(dir, "cache");
    const cache = path.join(cacheRoot, version, selected.key);
    await mkdir(cache, { recursive: true });
    await copyFile(archive, path.join(cache, selected.archive));
    await writeFile(path.join(cache, "checksums.txt"), `${createHash("sha256").update(bytes).digest("hex")}  ${selected.archive}\n`);
    const env = { ...process.env, SKILLEX_CACHE_DIR: cacheRoot, SKILLEX_OFFLINE: "1" };
    const wrapper = path.resolve("npm/skillex/bin/skillex.js");
    const run = (command, args, cwd = dir) => spawnSync(command, args, { cwd, env, encoding: "utf8" });
    const result = run(process.execPath, [wrapper, "--version"]);
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, new RegExp(version.replaceAll(".", "\\.")));
    const bad = run(process.execPath, [wrapper, "--not-a-skillex-flag"]);
    const directBad = run(binary, ["--not-a-skillex-flag"]);
    assert.notEqual(bad.status, 0, bad.stderr);
    assert.equal(bad.status, directBad.status);

    for (const strategy of ["npm", "pnpm", "source"]) {
      const project = path.join(dir, `${strategy} project`);
      await mkdir(project);
      const init = run(binary, ["init", "--yes", "--harness", "cursor", "--invocation", strategy], project);
      assert.equal(init.status, 0, init.stderr);
      const config = JSON.parse(await readFile(path.join(project, ".cursor/mcp.json"), "utf8"));
      const { command, args } = config.mcpServers.skillex;
      if (strategy === "source") {
        assert.equal(command, "./.skillex/bin/skillex.exe");
        await mkdir(path.join(project, ".skillex/bin"), { recursive: true });
        await copyFile(binary, path.join(project, ".skillex/bin/skillex.exe"));
      } else {
        assert.equal(command, "node");
        assert.deepEqual(args, ["./node_modules/@atheory-ai/skillex/bin/skillex.js", "mcp"]);
        const pkg = path.join(project, "node_modules/@atheory-ai/skillex");
        await mkdir(path.dirname(pkg), { recursive: true });
        await cp(path.resolve("npm/skillex"), pkg, { recursive: true });
      }
      // Use the exact generated executable and wrapper prefix; --version is a
      // deterministic invocation that exits rather than starting an MCP server.
      const invoked = run(command, [...args.slice(0, -1), "--version"], project);
      assert.equal(invoked.status, 0, invoked.stderr);
      assert.match(invoked.stdout, new RegExp(version.replaceAll(".", "\\.")));
      if (strategy !== "source") {
        await rm(path.join(project, "node_modules"), { recursive: true });
        const missing = run(command, [...args.slice(0, -1), "--version"], project);
        assert.notEqual(missing.status, 0, "missing local package must fail");
      }
    }
    await writeFile(path.join(cache, selected.archive), "tampered");
    const tampered = run(process.execPath, [wrapper, "--version"]);
    assert.notEqual(tampered.status, 0);
    assert.match(tampered.stderr, /checksum mismatch/);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
