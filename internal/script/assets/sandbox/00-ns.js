/*
 * postctl script sandbox.
 *
 * The files in this directory are concatenated in file-name order and
 * evaluated once per VM inside a single function scope, so every `var` here is
 * private to the sandbox and invisible to user scripts. Only what
 * 99-boot.js explicitly publishes on globalThis is reachable from a script.
 *
 * `host` is the Go bridge (see bridge.go). It is the sandbox's only way out.
 */
'use strict';

var NS = {};
var host = null;
var G = globalThis;
