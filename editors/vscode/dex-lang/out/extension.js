"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const path = __importStar(require("path"));
const vscode_1 = require("vscode");
const node_1 = require("vscode-languageclient/node");
const child_process_1 = require("child_process");
let client;
function resolveCommand(cmd) {
    if (cmd.includes("/") || cmd.includes("\\")) {
        return cmd; // already a path
    }
    try {
        if (process.platform === "win32") {
            return (0, child_process_1.execSync)(`where ${cmd}`).toString().split(/\r?\n/)[0].trim() || cmd;
        }
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
    const status = vscode_1.window.createStatusBarItem(vscode_1.StatusBarAlignment.Right, 100);
    status.command = "dex.showVersion";
    context.subscriptions.push(status);
    (0, child_process_1.execFile)(dexPath, ["version"], (err, stdout) => {
        if (err) {
            status.text = "$(warning) Dex";
            status.tooltip = `Could not run '${dexPath}'. Install dex or set dex.path.`;
            status.show();
            vscode_1.window.showErrorMessage(`Dex: could not run '${dexPath}'. Install dex or set dex.path.`);
            return;
        }
        const found = parseVersion(stdout);
        status.text = found ? `Dex ${found.join(".")}` : "Dex";
        status.tooltip = stdout.trim();
        status.show();
        if (!wanted || !found || !olderThan(found, wanted)) {
            return;
        }
        vscode_1.window.showWarningMessage(`Dex: the compiler on your PATH is ${found.join(".")}, older than this ` +
            `extension (${wanted.join(".")}). Diagnostics come from the compiler, ` +
            `so run 'dex update' and reload the window.`);
    });
}
let terminal;
function quote(arg) {
    if (/^[A-Za-z0-9_./:\\-]+$/.test(arg)) {
        return arg;
    }
    if (process.platform === "win32") {
        return `"${arg.replace(/"/g, '""')}"`;
    }
    return `'${arg.replace(/'/g, "'\\''")}'`;
}
function runInTerminal(dexPath, args, cwd) {
    if (!terminal || terminal.exitStatus !== undefined) {
        terminal = vscode_1.window.createTerminal({ name: "Dex", cwd });
    }
    const call = process.platform === "win32" ? `& ${quote(dexPath)}` : quote(dexPath);
    terminal.show(true);
    terminal.sendText(`cd ${quote(cwd)}`);
    terminal.sendText([call, ...args.map(quote)].join(" "));
}
async function activeDexFile(uri) {
    const editor = vscode_1.window.activeTextEditor;
    const target = uri ?? editor?.document.uri;
    if (!target || !target.fsPath.endsWith(".dx")) {
        vscode_1.window.showWarningMessage("Dex: open a .dx file first.");
        return undefined;
    }
    if (editor && editor.document.uri.toString() === target.toString() && editor.document.isDirty) {
        await editor.document.save();
    }
    return target.fsPath;
}
function projectDir(file) {
    const folder = vscode_1.workspace.getWorkspaceFolder(vscode_1.Uri.file(file));
    return folder ? folder.uri.fsPath : path.dirname(file);
}
function registerCommands(context, dexPath) {
    const onFile = (verb) => async (uri) => {
        const file = await activeDexFile(uri);
        if (file) {
            const cwd = projectDir(file);
            runInTerminal(dexPath, verb(path.relative(cwd, file) || file), cwd);
        }
    };
    context.subscriptions.push(vscode_1.commands.registerCommand("dex.run", onFile((f) => (f.endsWith("_test.dx") ? ["test", f] : ["run", f]))), vscode_1.commands.registerCommand("dex.build", onFile((f) => ["build", f])), vscode_1.commands.registerCommand("dex.test", onFile((f) => ["test", f])), vscode_1.commands.registerCommand("dex.lint", onFile((f) => ["lint", f])), vscode_1.commands.registerCommand("dex.dev", onFile((f) => ["dev", f])), vscode_1.commands.registerCommand("dex.testAll", async () => {
        await vscode_1.workspace.saveAll(false);
        const folder = vscode_1.workspace.workspaceFolders?.[0];
        if (!folder) {
            vscode_1.window.showWarningMessage("Dex: open a folder to run its tests.");
            return;
        }
        runInTerminal(dexPath, ["test"], folder.uri.fsPath);
    }), vscode_1.commands.registerCommand("dex.showVersion", () => {
        (0, child_process_1.execFile)(dexPath, ["version"], (err, stdout) => {
            if (err) {
                vscode_1.window.showErrorMessage(`Dex: could not run '${dexPath}'.`);
                return;
            }
            vscode_1.window.showInformationMessage(`${stdout.trim()} (${dexPath})`, "Update").then((pick) => {
                if (pick === "Update") {
                    runInTerminal(dexPath, ["update"], vscode_1.workspace.workspaceFolders?.[0]?.uri.fsPath ?? process.cwd());
                }
            });
        });
    }), vscode_1.commands.registerCommand("dex.restartServer", async () => {
        if (client) {
            await client.restart();
        }
    }));
}
function activate(context) {
    const config = vscode_1.workspace.getConfiguration("dex");
    const dexPath = resolveCommand(config.get("path", "dex"));
    checkCompilerVersion(dexPath, context);
    registerCommands(context, dexPath);
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