#!/usr/bin/env node
"use strict";

const { target, acquire } = require("./acquire");
try {
  target();
} catch (error) {
  process.stderr.write(`${error.message}\n`);
  process.exit(1);
}

if (process.env.SKILLEX_SKIP_DOWNLOAD !== "1") {
  acquire().catch((error) => {
    // A package install may happen offline or on a build host. Runtime retries.
    process.stderr.write(`${error.message}\nSkillex will retry acquisition on first use. Set SKILLEX_SKIP_DOWNLOAD=1 to skip postinstall acquisition.\n`);
  });
}
