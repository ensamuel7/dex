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
function activate(context) {
    const config = vscode_1.workspace.getConfiguration("dex");
    const dexPath = resolveCommand(config.get("path", "dex"));
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