import * as App from '../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOn, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';

/** api is the backend's bound methods; data comes back through on(). */
export const api = {
  ready: (): Promise<void> => App.Ready(),
  version: (): Promise<string> => App.Version(),
  platform: (): Promise<string> => App.Platform(),
  mkvmergeInfo: (): Promise<string> => App.MkvmergeInfo(),
  mkvmergeSourceURL: (): Promise<string> => App.MkvmergeSourceURL(),
  addPaths: (paths: string[]): Promise<void> => App.AddPaths(paths),
  pickISOs: (): Promise<void> => App.PickISOs(),
  pickFolder: (): Promise<void> => App.PickFolder(),
  removeDisc: (path: string): Promise<void> => App.RemoveDisc(path),
  pickOutputDir: (path: string): Promise<void> => App.PickOutputDir(path),
  enqueue: (path: string, titleIds: string[], names: string[]): Promise<string[] | null> =>
    App.Enqueue(path, titleIds, names),
  rename: (id: string, name: string): Promise<string> => App.Rename(id, name),
  move: (id: string, index: number): Promise<void> => App.Move(id, index),
  remove: (id: string): Promise<void> => App.Remove(id),
  cancel: (id: string): Promise<void> => App.Cancel(id),
  retry: (id: string): Promise<void> => App.Retry(id),
  clearFinished: (): Promise<void> => App.ClearFinished(),
  setPaused: (paused: boolean): Promise<void> => App.SetPaused(paused),
  recheckMkvmerge: (): Promise<void> => App.RecheckMkvmerge(),
  /** checkForUpdate asks GitHub now; it resolves to the newer tag, or '' when current, and rejects with a message. */
  checkForUpdate: (): Promise<string> => App.CheckForUpdate(),
  /** upgradeHint is the package manager command that upgrades this install, or '' for a direct download. */
  upgradeHint: (): Promise<string> => App.UpgradeHint(),
  reloadConfig: (): Promise<void> => App.ReloadConfig(),
  reveal: (id: string): Promise<string> => App.Reveal(id),
  /** openURL opens url in the system browser; the app's window never navigates. */
  openURL: (url: string): void => BrowserOpenURL(url),
};

/** on subscribes to a backend event; it returns the unsubscribe function. */
export function on<T>(event: string, cb: (value: T) => void): () => void {
  return EventsOn(event, (...data: unknown[]) => cb(data[0] as T));
}

/** onFileDrop reports paths dropped anywhere on the window. */
export function onFileDrop(cb: (paths: string[]) => void): void {
  OnFileDrop((_x: number, _y: number, paths: string[]) => cb(paths), false);
}

export function offFileDrop(): void {
  OnFileDropOff();
}
