import { workspace, ExtensionContext } from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient;

export function activate(context: ExtensionContext) {
  const config = workspace.getConfiguration("dex");
  const dexPath = config.get<string>("path", "dex");

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
