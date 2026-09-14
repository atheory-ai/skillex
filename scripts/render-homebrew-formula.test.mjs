import assert from "node:assert/strict";
import test from "node:test";

import { renderHomebrewFormula } from "./render-homebrew-formula.mjs";

const checksums = [
  ["a".repeat(64), "skillex_0.9.0_darwin_amd64.tar.gz"],
  ["b".repeat(64), "skillex_0.9.0_darwin_arm64.tar.gz"],
  ["c".repeat(64), "skillex_0.9.0_linux_amd64.tar.gz"],
  ["d".repeat(64), "skillex_0.9.0_linux_arm64.tar.gz"],
  ["e".repeat(64), "skillex_0.9.0_windows_amd64.zip"],
].map(([checksum, filename]) => `${checksum}  ${filename}`).join("\n");

test("renders all supported Homebrew release archives", () => {
  const formula = renderHomebrewFormula("0.9.0", checksums);
  assert.match(formula, /class Skillex < Formula/);
  assert.match(formula, /version "0\.9\.0"/);
  assert.equal((formula.match(/https:\/\/github\.com\/atheory-ai\/skillex\/releases/g) || []).length, 4);
  assert.match(formula, new RegExp(`sha256 "${"a".repeat(64)}"`));
  assert.match(formula, new RegExp(`sha256 "${"d".repeat(64)}"`));
  assert.doesNotMatch(formula, /windows/);
});

test("rejects unsafe versions and incomplete checksums", () => {
  assert.throws(() => renderHomebrewFormula("0.9.0; echo unsafe", checksums), /invalid release version/);
  assert.throws(() => renderHomebrewFormula("0.9.0", checksums.split("\n").slice(0, 3).join("\n")), /missing checksum/);
  assert.throws(() => renderHomebrewFormula("0.9.0", "not a checksum"), /invalid checksum line/);
});
