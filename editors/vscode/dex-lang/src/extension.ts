import { workspace, ExtensionContext, window } from "vscode";
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
  execFile(dexPath, ["version"], (err, stdout) => {
    if (err) {
      window.showErrorMessage(
        `Dex: could not run '${dexPath}'. Install dex or set dex.path.`
      );
      return;
    }
    const found = parseVersion(stdout);
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

export function activate(context: ExtensionContext) {
  const config = workspace.getConfiguration("dex");
  const dexPath = resolveCommand(config.get<string>("path", "dex"));

  checkCompilerVersion(dexPath, context);

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
