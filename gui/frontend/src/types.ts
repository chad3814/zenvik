export interface TitleSummary {
  id: string;
  durationSeconds: number;
  chapters: number;
  sizeBytes: number;
  main: boolean;
  rippable: boolean;
  reason?: string;
  skippedCells: string[];
  defaultName: string;
}

export interface DiscSummary {
  path: string;
  state: 'opening' | 'ready' | 'error';
  error?: string;
  errorLabel?: string;
  format?: string;
  label?: string;
  name?: string;
  ambiguous: boolean;
  outputDir: string;
  titles: TitleSummary[];
}

export type EntryState = 'waiting' | 'ripping' | 'done' | 'failed' | 'canceled';

export interface Entry {
  id: string;
  discPath: string;
  titleId: string;
  outputPath: string;
  state: EntryState;
  message?: string;
  label?: string;
  addedAt: string;
  startedAt?: string;
  endedAt?: string;
}

export interface QueueSnapshot {
  entries: Entry[];
  paused: boolean;
  ready: boolean;
  saveError?: string;
}

export interface Progress {
  id: string;
  phase: string;
  fraction: number;
  bytesDone: number;
  bytesTotal: number;
}

export interface Banner {
  id: string;
  message: string;
  action?: 'recheck';
}
