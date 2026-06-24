import { workspace, ExtensionContext, window } from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";
import { execSync } from "child_process";

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

export function activate(context: ExtensionContext) {
  const config = workspace.getConfiguration("dex");
  const dexPath = resolveCommand(config.get<string>("path", "dex"));

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
