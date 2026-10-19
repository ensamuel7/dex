"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const vscode_1 = require("vscode");
const node_1 = require("vscode-languageclient/node");
const child_process_1 = require("child_process");
let client;
function resolveCommand(cmd) {
    if (cmd.includes("/") || cmd.includes("\\")) {
        return cmd; // already a path
    }
    try {
        // resolve from user's login shell to get full PATH
        return (0, child_process_1.execSync)(`/bin/sh -lc 'which ${cmd}'`).toString().trim();
    }
    catch {
        return cmd;
    }
}
function parseVersion(text) {
    const m = text.match(/(\d+)\.(\d+)\.(\d+)/);
    return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : undefined;
}
function olderThan(a, b) {
    for (let i = 0; i < 3; i++) {
        if (a[i] !== b[i]) {
            return a[i] < b[i];
        }
    }
    return false;
}
function checkCompilerVersion(dexPath, context) {
    const wanted = parseVersion(context.extension.packageJSON.version);
    (0, child_process_1.execFile)(dexPath, ["version"], (err, stdout) => {
        if (err) {
            vscode_1.window.showErrorMessage(`Dex: could not run '${dexPath}'. Install dex or set dex.path.`);
            return;
        }
        const found = parseVersion(stdout);
        if (!wanted || !found || !olderThan(found, wanted)) {
            return;
        }
        vscode_1.window.showWarningMessage(`Dex: the compiler on your PATH is ${found.join(".")}, older than this ` +
            `extension (${wanted.join(".")}). Diagnostics come from the compiler, ` +
            `so run 'dex update' and reload the window.`);
    });
}
function activate(context) {
    const config = vscode_1.workspace.getConfiguration("dex");
    const dexPath = resolveCommand(config.get("path", "dex"));
    checkCompilerVersion(dexPath, context);
    const serverOptions = {
        command: dexPath,
        args: ["lsp"],
    };
    const clientOptions = {
        documentSelector: [{ scheme: "file", language: "dex" }],
        synchronize: {
            fileEvents: vscode_1.workspace.createFileSystemWatcher("**/*.dx"),
        },
    };
    client = new node_1.LanguageClient("dexLanguageServer", "Dex Language Server", serverOptions, clientOptions);
    client.start();
}
function deactivate() {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
//# sourceMappingURL=extension.js.map