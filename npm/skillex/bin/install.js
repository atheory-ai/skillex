#!/usr/bin/env node
"use strict";

if (process.env.SKILLEX_SKIP_DOWNLOAD !== "1") {
  require("./acquire").acquire().catch((error) => {
    // A package install may happen offline or on a build host. Runtime retries.
    process.stderr.write(`${error.message}\nSkillex will retry acquisition on first use. Set SKILLEX_SKIP_DOWNLOAD=1 to skip postinstall acquisition.\n`);
  });
}
