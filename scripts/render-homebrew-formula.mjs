import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const platforms = [
  ["darwin", "amd64"],
  ["darwin", "arm64"],
  ["linux", "amd64"],
  ["linux", "arm64"],
];

export function renderHomebrewFormula(version, checksumsText) {
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error(`invalid release version: ${version}`);
  }

  const checksums = new Map();
  for (const line of checksumsText.trim().split(/\r?\n/)) {
    const match = line.match(/^([a-f0-9]{64})  ([A-Za-z0-9._-]+)$/);
    if (!match) {
      throw new Error(`invalid checksum line: ${line}`);
    }
    checksums.set(match[2], match[1]);
  }

  const stanza = (os, arch) => {
    const filename = `skillex_${version}_${os}_${arch}.tar.gz`;
    const checksum = checksums.get(filename);
    if (!checksum) {
      throw new Error(`missing checksum for ${filename}`);
    }
    return `      url "https://github.com/atheory-ai/skillex/releases/download/v${version}/${filename}"
      sha256 "${checksum}"`;
  };

  return `class Skillex < Formula
  desc "Skill management for AI agents"
  homepage "https://github.com/atheory-ai/skillex"
  version "${version}"
  license "Apache-2.0"

  on_macos do
    on_intel do
${stanza("darwin", "amd64")}
    end
    on_arm do
${stanza("darwin", "arm64")}
    end
  end

  on_linux do
    on_intel do
${stanza("linux", "amd64")}
    end
    on_arm do
${stanza("linux", "arm64")}
    end
  end

  def install
    bin.install "skillex"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/skillex version")
  end
end
`;
}

function main() {
  const [version, checksumsPath, outputPath] = process.argv.slice(2);
  if (!version || !checksumsPath || !outputPath) {
    throw new Error("usage: render-homebrew-formula.mjs <version> <checksums.txt> <output.rb>");
  }
  const formula = renderHomebrewFormula(version, readFileSync(checksumsPath, "utf8"));
  mkdirSync(dirname(outputPath), { recursive: true });
  writeFileSync(outputPath, formula);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main();
}
