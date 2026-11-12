import * as path from "path";
import {
  commands,
  ExtensionContext,
  StatusBarAlignment,
  Terminal,
  Uri,
  window,
  workspace,
} from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";
import { execFile, execSync } from "child_process";

let client: LanguageClient;

function resolveCommand(cmd: string): string {
  if (cmd.includes("/") || cmd.includes("\\")) {
    return cmd; // already a path
  }
  try {
    if (process.platform === "win32") {
      return execSync(`where ${cmd}`).toString().split(/\r?\n/)[0].trim() || cmd;
    }
    // resolve from user's login shell to get full PATH
    return execSync(`/bin/sh -lc 'which ${cmd}'`).toString().trim();
  } catch {
    return cmd;
  }
}

function parseVersion(text: string): number[] | undefined {
  const m = text.match(/(\d+)\.(\d+)\.(\d+)/);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : undefined;
}

function olderThan(a: number[], b: number[]): boolean {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) {
      return a[i] < b[i];
    }
  }
  return false;
}

function checkCompilerVersion(dexPath: string, context: ExtensionContext) {
  const wanted = parseVersion(context.extension.packageJSON.version);
  const status = window.createStatusBarItem(StatusBarAlignment.Right, 100);
  status.command = "dex.showVersion";
  context.subscriptions.push(status);
  execFile(dexPath, ["version"], (err, stdout) => {
    if (err) {
      status.text = "$(warning) Dex";
      status.tooltip = `Could not run '${dexPath}'. Install dex or set dex.path.`;
      status.show();
      window.showErrorMessage(
        `Dex: could not run '${dexPath}'. Install dex or set dex.path.`
      );
      return;
    }
    const found = parseVersion(stdout);
    status.text = found ? `Dex ${found.join(".")}` : "Dex";
    status.tooltip = stdout.trim();
    status.show();
    if (!wanted || !found || !olderThan(found, wanted)) {
      return;
    }
    window.showWarningMessage(
      `Dex: the compiler on your PATH is ${found.join(".")}, older than this ` +
        `extension (${wanted.join(".")}). Diagnostics come from the compiler, ` +
        `so run 'dex update' and reload the window.`
    );
  });
}

let terminal: Terminal | undefined;

function quote(arg: string): string {
  if (/^[A-Za-z0-9_./:\\-]+$/.test(arg)) {
    return arg;
  }
  if (process.platform === "win32") {
    return `"${arg.replace(/"/g, '""')}"`;
  }
  return `'${arg.replace(/'/g, "'\\''")}'`;
}

function runInTerminal(dexPath: string, args: string[], cwd: string) {
  if (!terminal || terminal.exitStatus !== undefined) {
    terminal = window.createTerminal({ name: "Dex", cwd });
  }
  const call = process.platform === "win32" ? `& ${quote(dexPath)}` : quote(dexPath);
  terminal.show(true);
  terminal.sendText(`cd ${quote(cwd)}`);
  terminal.sendText([call, ...args.map(quote)].join(" "));
}

async function activeDexFile(uri?: Uri): Promise<string | undefined> {
  const editor = window.activeTextEditor;
  const target = uri ?? editor?.document.uri;
  if (!target || !target.fsPath.endsWith(".dx")) {
    window.showWarningMessage("Dex: open a .dx file first.");
    return undefined;
  }
  if (editor && editor.document.uri.toString() === target.toString() && editor.document.isDirty) {
    await editor.document.save();
  }
  return target.fsPath;
}

function projectDir(file: string): string {
  const folder = workspace.getWorkspaceFolder(Uri.file(file));
  return folder ? folder.uri.fsPath : path.dirname(file);
}

function registerCommands(context: ExtensionContext, dexPath: string) {
  const onFile = (verb: (file: string) => string[]) => async (uri?: Uri) => {
    const file = await activeDexFile(uri);
    if (file) {
      const cwd = projectDir(file);
      runInTerminal(dexPath, verb(path.relative(cwd, file) || file), cwd);
    }
  };
  context.subscriptions.push(
    commands.registerCommand(
      "dex.run",
      onFile((f) => (f.endsWith("_test.dx") ? ["test", f] : ["run", f]))
    ),
    commands.registerCommand("dex.build", onFile((f) => ["build", f])),
    commands.registerCommand("dex.test", onFile((f) => ["test", f])),
    commands.registerCommand("dex.lint", onFile((f) => ["lint", f])),
    commands.registerCommand("dex.dev", onFile((f) => ["dev", f])),
    commands.registerCommand("dex.testAll", async () => {
      await workspace.saveAll(false);
      const folder = workspace.workspaceFolders?.[0];
      if (!folder) {
        window.showWarningMessage("Dex: open a folder to run its tests.");
        return;
      }
      runInTerminal(dexPath, ["test"], folder.uri.fsPath);
    }),
    commands.registerCommand("dex.showVersion", () => {
      execFile(dexPath, ["version"], (err, stdout) => {
        if (err) {
          window.showErrorMessage(`Dex: could not run '${dexPath}'.`);
          return;
        }
        window.showInformationMessage(`${stdout.trim()} (${dexPath})`, "Update").then((pick) => {
          if (pick === "Update") {
            runInTerminal(dexPath, ["update"], workspace.workspaceFolders?.[0]?.uri.fsPath ?? process.cwd());
          }
        });
      });
    }),
    commands.registerCommand("dex.restartServer", async () => {
      if (client) {
        await client.restart();
      }
    })
  );
}

export function activate(context: ExtensionContext) {
  const config = workspace.getConfiguration("dex");
  const dexPath = resolveCommand(config.get<string>("path", "dex"));

  checkCompilerVersion(dexPath, context);
  registerCommands(context, dexPath);

  const serverOptions: ServerOptions = {
    command: dexPath,
    args: ["lsp"],
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "dex" }],
    synchronize: {
      fileEvents: workspace.createFileSystemWatcher("**/*.dx"),
    },
  };

  client = new LanguageClient(
    "dexLanguageServer",
    "Dex Language Server",
    serverOptions,
    clientOptions
  );

  client.start();
}

export function deactivate(): Thenable<void> | undefined {
  if (!client) {
    return undefined;
  }
  return client.stop();
}
