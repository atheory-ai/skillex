"use strict";

const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const https = require("node:https");
const { createHash } = require("node:crypto");
const { execFileSync } = require("node:child_process");
const { version } = require("../package.json");

const TARGETS = {
  "darwin-arm64": ["darwin", "arm64", "tar.gz"],
  "darwin-x64": ["darwin", "amd64", "tar.gz"],
  "linux-arm64": ["linux", "arm64", "tar.gz"],
  "linux-x64": ["linux", "amd64", "tar.gz"],
  "win32-x64": ["windows", "amd64", "zip"],
};

function target(platform = process.platform, arch = process.arch) {
  const key = `${platform}-${arch}`;
  if (!TARGETS[key]) throw new Error(`skillex: unsupported platform "${key}". Supported platforms: ${Object.keys(TARGETS).join(", ")}`);
  const [goos, goarch, extension] = TARGETS[key];
  return { key, archive: `skillex_${version}_${goos}_${goarch}.${extension}`, binary: platform === "win32" ? "skillex.exe" : "skillex" };
}

// GitHub redirects release downloads to its asset CDN. Never downgrade to HTTP.
function download(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:") return reject(new Error("skillex: release downloads require HTTPS"));
    const req = https.get(parsed, { headers: { "User-Agent": `skillex-npm/${version}` } }, (res) => {
      if ([301, 302, 303, 307, 308].includes(res.statusCode)) {
        res.resume();
        if (redirects >= 5 || !res.headers.location) return reject(new Error("skillex: too many release redirects"));
        resolve(download(new URL(res.headers.location, parsed).href, redirects + 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`skillex: download failed (${res.statusCode}) for ${url}`));
      }
      const chunks = [];
      let size = 0;
      res.on("data", (chunk) => {
        size += chunk.length;
        if (size > 100 * 1024 * 1024) res.destroy(new Error("skillex: release asset exceeds 100 MiB"));
        else chunks.push(chunk);
      });
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    });
    req.setTimeout(30000, () => req.destroy(new Error("skillex: release download timed out")));
    req.on("error", reject);
  });
}

function verify(archive, checksums, filename) {
  const rows = checksums.toString("utf8").split(/\r?\n/).map((line) => line.match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/)).filter((row) => row && row[2] === filename);
  if (rows.length !== 1) throw new Error(`skillex: expected exactly one SHA-256 checksum for ${filename}`);
  if (createHash("sha256").update(archive).digest("hex") !== rows[0][1].toLowerCase()) throw new Error(`skillex: SHA-256 checksum mismatch for ${filename}`);
}

async function acquire(options = {}) {
  const selected = options.target || target();
  const cacheRoot = options.cacheRoot || process.env.SKILLEX_CACHE_DIR || path.join(process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"), "skillex");
  const cache = path.join(cacheRoot, version, selected.key);
  const offline = options.offline ?? process.env.SKILLEX_OFFLINE === "1";
  const fetch = options.download || download;
  let archive;
  let checksums;
  try {
    [archive, checksums] = await Promise.all([fs.readFile(path.join(cache, selected.archive)), fs.readFile(path.join(cache, "checksums.txt"))]);
    verify(archive, checksums, selected.archive);
  } catch (error) {
    if (offline) throw new Error(`skillex: no verified cached release for ${version}/${selected.key} in ${cache}. SKILLEX_OFFLINE=1 prevents downloads. ${error.message}`);
    const base = `https://github.com/atheory-ai/skillex/releases/download/v${version}`;
    [archive, checksums] = await Promise.all([fetch(`${base}/${selected.archive}`), fetch(`${base}/checksums.txt`)]);
    verify(archive, checksums, selected.archive);
    await fs.mkdir(cache, { recursive: true });
    // Unique staging names avoid partial reads and collisions between npm invocations.
    const staging = await fs.mkdtemp(path.join(cache, ".download-"));
    try {
      await fs.writeFile(path.join(staging, selected.archive), archive);
      await fs.writeFile(path.join(staging, "checksums.txt"), checksums);
      await fs.rename(path.join(staging, selected.archive), path.join(cache, selected.archive));
      await fs.rename(path.join(staging, "checksums.txt"), path.join(cache, "checksums.txt"));
    } finally {
      await fs.rm(staging, { recursive: true, force: true });
    }
  }
  return { archive, selected };
}

async function prepareBinary(options = {}) {
  const { archive, selected } = await acquire(options);
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "skillex-npm-"));
  try {
    const archivePath = path.join(directory, selected.archive);
    await fs.writeFile(archivePath, archive);
    // Windows 10+ includes bsdtar, which also reads ZIP. Extract only the binary.
    execFileSync("tar", ["-xf", archivePath, "-C", directory, selected.binary], { stdio: "pipe" });
    const binary = path.join(directory, selected.binary);
    const stat = await fs.lstat(binary);
    if (!stat.isFile()) throw new Error("skillex: release binary is not a regular file");
    await fs.chmod(binary, 0o755);
    return { binary, cleanup: () => fs.rm(directory, { recursive: true, force: true }) };
  } catch (error) {
    await fs.rm(directory, { recursive: true, force: true });
    throw new Error(`skillex: cannot extract verified release (tar is required): ${error.message}`);
  }
}

module.exports = { target, verify, acquire, prepareBinary, TARGETS };
