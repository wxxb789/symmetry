import { cp, mkdir, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const staging = fileURLToPath(new URL("../dist/", import.meta.url));
const destination = fileURLToPath(new URL("../../control/priv/static/portal/", import.meta.url));

// This directory contains only this build's generated release assets.
await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
await cp(staging, destination, { recursive: true });
