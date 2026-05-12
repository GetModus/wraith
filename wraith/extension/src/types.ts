export type ChromeTab = {
  id?: number;
  title?: string;
  url?: string;
  active?: boolean;
  windowId?: number;
};

export type BridgeCommandMessage = {
  id: string;
  command: "ping" | "get_tabs" | "navigate" | "extract_page" | "capture_page" | "scrape_source" | "query_source";
  params?: Record<string, unknown>;
};

export type LegacySourceMessage = {
  id: string;
  type: "scrape" | "query";
  source: string;
  query?: string;
  params?: Record<string, unknown>;
};

export type BridgeResult = {
  type: "command_result";
  payload: {
    id: string;
    ok: boolean;
    result?: unknown;
    error?: string;
  };
};

export type PageSnapshot = {
  title: string;
  url: string;
  text: string;
  selection: string;
  description: string;
};

export type SourceResult = {
  success: boolean;
  data?: unknown;
  error?: string;
};

type TabQuery = Record<string, unknown>;

declare global {
  const chrome: {
    runtime: {
      id: string;
      onMessage: {
        addListener(listener: (message: unknown, sender: unknown, sendResponse: (response: unknown) => void) => boolean): void;
      };
    };
    tabs: {
      query(queryInfo: TabQuery): Promise<ChromeTab[]>;
      sendMessage(tabId: number, message: unknown): Promise<unknown>;
      update(tabId: number, updateProperties: { url?: string; active?: boolean }): Promise<ChromeTab>;
    };
  };
}
