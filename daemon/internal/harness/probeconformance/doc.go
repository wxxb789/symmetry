// Package probeconformance owns the probe contracts that the Codex, OpenCode,
// and pi adapters must satisfy identically. Every contract is one table with a
// row per provider, so a provider can only differ in the data actually under
// test: its exported probe entry point, its injected command runner, and its
// version transcript.
//
// The contracts live in their own package because each adapter keeps its probe
// seams package-private. A provider table here keeps one owner of the shared
// expectations instead of one private copy per adapter.
package probeconformance
