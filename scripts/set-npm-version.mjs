#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";

const version = process.argv[2] ?? (await readFile("VERSION", "utf8")).trim();

if (!version) {
  console.error("usage: node scripts/set-npm-version.mjs <version>");
  process.exit(1);
}

const packageFiles = [
  "npm/skillex/package.json",
  "npm/darwin-arm64/package.json",
  "npm/darwin-x64/package.json",
  "npm/linux-arm64/package.json",
  "npm/linux-x64/package.json",
  "npm/win32-x64/package.json",
];

for (const file of packageFiles) {
  const raw = await readFile(file, "utf8");
  const pkg = JSON.parse(raw);
  pkg.version = version;

  if (pkg.optionalDependencies) {
    for (const dependency of Object.keys(pkg.optionalDependencies)) {
      pkg.optionalDependencies[dependency] = version;
    }
  }

  await writeFile(file, `${JSON.stringify(pkg, null, 2)}\n`);
}
