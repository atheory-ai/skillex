#!/usr/bin/env node
"use strict";

// This script is the platform selector shim for @atheory-ai/skillex.
//
// npm installs only the optionalDependency that matches the current
// platform (via the "os" and "cpu" fields in each platform package).
// This script resolves which platform package was installed, locates
// its binary, and exec's it — passing all arguments through.

const { execFileSync } = require("child_process");
const path = require("path");

// Maps process.platform + process.arch to the npm package name.
const PLATFORM_PACKAGES = {
  "darwin-arm64": "@atheory-ai/skillex-darwin-arm64",
  "darwin-x64":   "@atheory-ai/skillex-darwin-x64",
  "linux-x64":    "@atheory-ai/skillex-linux-x64",
  "linux-arm64":  "@atheory-ai/skillex-linux-arm64",
  "win32-x64":    "@atheory-ai/skillex-win32-x64",
};

function getBinaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const packageName = PLATFORM_PACKAGES[key];

  if (!packageName) {
    throw new Error(
      `skillex: unsupported platform "${key}"\n` +
      `Supported platforms: ${Object.keys(PLATFORM_PACKAGES).join(", ")}\n` +
      `\nPlease open an issue at https://github.com/atheory-ai/skillex/issues`
    );
  }

  let packageDir;
  try {
    // require.resolve finds the package.json of the platform package,
    // which npm installed (or skipped if the platform didn't match).
    packageDir = path.dirname(require.resolve(`${packageName}/package.json`));
  } catch {
    throw new Error(
      `skillex: the platform package "${packageName}" is not installed.\n` +
      `\nThis usually happens when optional dependencies were skipped. Try:\n` +
      `  npm install --include=optional\n` +
      `  pnpm install\n` +
      `  yarn install`
    );
  }

  const isWindows = process.platform === "win32";
  return path.join(packageDir, "bin", isWindows ? "skillex.exe" : "skillex");
}

let binaryPath;
try {
  binaryPath = getBinaryPath();
} catch (err) {
  process.stderr.write(err.message + "\n");
  process.exit(1);
}

try {
  execFileSync(binaryPath, process.argv.slice(2), {
    stdio: "inherit",
    windowsHide: false,
  });
} catch (err) {
  // execFileSync throws when the child exits non-zero.
  // The child already printed its own output; we just mirror the exit code.
  process.exit(err.status ?? 1);
}
