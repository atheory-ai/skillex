#!/usr/bin/env node
"use strict";

const { spawn } = require("node:child_process");
const { prepareBinary } = require("./acquire");

(async () => {
  const { binary, cleanup } = await prepareBinary();
  const child = spawn(binary, process.argv.slice(2), { stdio: "inherit", windowsHide: false });
  const forward = (signal) => () => child.kill(signal);
  const interrupt = forward("SIGINT");
  const terminate = forward("SIGTERM");
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", terminate);
  child.once("error", async (error) => {
    await cleanup();
    process.stderr.write(`skillex: ${error.message}\n`);
    process.exitCode = 1;
  });
  child.once("exit", async (code, signal) => {
    process.removeListener("SIGINT", interrupt);
    process.removeListener("SIGTERM", terminate);
    await cleanup();
    if (signal) process.kill(process.pid, signal);
    else process.exitCode = code ?? 1;
  });
})().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
