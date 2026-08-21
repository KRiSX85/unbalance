export enum Op {
  Neutral = 0,
  ScatterPlan = 1,
  ScatterMove = 2,
  ScatterCopy = 3,
  ScatterValidate = 4,
  GatherPlan = 5,
  GatherMove = 6,
  AutoGatherDryRun = 7,
  AutoGatherReal = 8,
}

export type Step = 'idle' | 'select' | 'plan' | 'transfer';

export type Variant = 'primary' | 'secondary' | 'accent';

export interface Config {
  version: string;
  dryRun: boolean;
  notifyPlan: number;
  notifyTransfer: number;
  reservedAmount: number;
  reservedUnit: string;
  rsyncArgs: string[];
  verbosity: number;
  refreshRate: number;
  logLines: number;
  speedWindow: string;
  tvLibraryPath: string;
  authEnabled: boolean;
  authUsername: string;
}

export interface AutoGatherDiskPresence {
  diskName: string;
  videoCount: number;
  videoBytes: number;
  totalBytes: number;
  emptyOnly: boolean;
  sidecarOnly: boolean;
}

export interface AutoGatherShow {
  name: string;
  path: string;
  status: string;
  split: boolean;
  ready: boolean;
  totalVideoBytes: number;
  totalBytes: number;
  videoDisks: AutoGatherDiskPresence[];
  sidecarOnlyDisks: AutoGatherDiskPresence[];
  emptyOnlyDisks: AutoGatherDiskPresence[];
  cachePoolsWithVideo: string[];

  cleanupCandidateDisks?: string[];
  cleanupCandidateCount?: number;

  recommendedTargetDisk?: string;
  moveRequiredBytes?: number;
  projectedFreeBytes?: number;
  projectedFreePercent?: number;
  belowPreferredFreeFloor?: boolean;
  minMovementAlternative?: AutoGatherTargetCandidate;
  gatherTargets?: AutoGatherTargetCandidate[];
  noEligibleReason?: string;
}

export interface AutoGatherTargetCandidate {
  diskName: string;
  eligible: boolean;
  ineligibleReason?: string;
  moveRequiredBytes: number;
  currentShowBytesOnTarget: number;
  freeBytes: number;
  diskSizeBytes: number;
  projectedFreeBytes: number;
  projectedFreePercent: number;
  meetsPreferredFreeFloor: boolean;
}

export interface AutoGatherScanResult {
  libraryPath: string;
  shows: AutoGatherShow[];
  warnings?: string[];
  error?: string;
  cancelled?: boolean;
  revision?: number;
}

export interface AutoGatherCanonicalTarget {
  diskName: string;
  diskPath: string;
  isPhysicalArrayDisk: boolean;
  canonicalEligible: boolean;
  ineligibleReason?: string;
  canonicalBytesToMove: number;
  canonicalCurrentBytesOnTarget: number;
  canonicalItemCount: number;
  freeBytes: number;
  diskSizeBytes: number;
  projectedFreeBytes: number;
  projectedFreePercent: number;
  meetsPreferredFreeFloor: boolean;
  rawGatherBinPresent: boolean;
}

export interface AutoGatherCanonicalPlanResult {
  showPath: string;
  stage2RecommendedTarget?: string;
  stage2EstimatedMoveBytes?: number;
  stage2TargetStillCanonicalEligible: boolean;
  canonicalRecommendedTarget?: string;
  canonicalMoveBytes?: number;
  canonicalProjectedFreeBytes?: number;
  canonicalProjectedFreePercent?: number;
  belowPreferredFreeFloor?: boolean;
  canonicalItemCountTotal?: number;
  canonicalTargets?: AutoGatherCanonicalTarget[];
  noEligibleReason?: string;
  error?: string;
  cancelled?: boolean;
}

export interface AutoGatherDryRunShowRecord {
  showPath: string;
  showName?: string;
  targetDisk?: string;
  moveBytes?: number;
  belowPreferredFreeFloor?: boolean;
  reason?: string;
  at?: string;
}

export interface AutoGatherDryRunState {
  phase: string;
  dryRun: boolean;
  currentShow?: string;
  currentShowName?: string;
  currentTarget?: string;
  completed?: AutoGatherDryRunShowRecord[];
  skipped?: AutoGatherDryRunShowRecord[];
  failedShow?: string;
  failedShowName?: string;
  failureReason?: string;
  startedAt?: string;
  endedAt?: string;
  iterationsConsidered?: number;
  splitRemaining?: number;
  message?: string;
  error?: string;
}

export interface AutoGatherRealPrepareResult {
  preparationId: string;
  showPath: string;
  showName?: string;
  sourceDisks?: string[];
  canonicalTargetDisk?: string;
  stage2RecommendedTarget?: string;
  stage2AgreesWithCanonical: boolean;
  currentBytesOnTarget?: number;
  estimatedMoveBytes?: number;
  targetFreeBytes?: number;
  projectedTargetFreeBytes?: number;
  executable: boolean;
  issues?: string[];
  permissionWarnings?: AutoGatherRealPermissionWarnings;
  emptyFolderOnlyDisks?: string[];
  expiresAt?: string;
  globalDryRun: boolean;
  error?: string;
}

export interface AutoGatherRealPermissionWarnings {
  ownerIssues?: number;
  groupIssues?: number;
  folderIssues?: number;
  fileIssues?: number;
}

export interface AutoGatherRealVerification {
  passed: boolean;
  targetDisk?: string;
  substantiveDisks?: string[];
  emptyFolderRemnants?: string[];
  message?: string;
}

export interface AutoGatherRealState {
  phase: string;
  globalDryRun: boolean;
  currentShow?: string;
  currentShowName?: string;
  currentTarget?: string;
  operationPhase?: string;
  preparationId?: string;
  prepared?: AutoGatherRealPrepareResult;
  verification?: AutoGatherRealVerification;
  startedAt?: string;
  endedAt?: string;
  error?: string;
  message?: string;
  stoppedMessage?: string;
}

export interface AutoGatherControlledShowRecord {
  showPath: string;
  showName?: string;
  targetDisk?: string;
  moveBytes?: number;
  permissionWarnings?: AutoGatherRealPermissionWarnings;
  reason?: string;
  at?: string;
}

export interface AutoGatherControlledRsyncProbe {
  pid?: number;
  alive?: boolean;
  plausibleRsync?: boolean;
  command?: string;
  note?: string;
}

export interface AutoGatherControlledState {
  phase: string;
  globalDryRun: boolean;
  maxShows: number;
  maxBytes: number;
  currentShow?: string;
  currentShowName?: string;
  currentTarget?: string;
  operationPhase?: string;
  completed?: AutoGatherControlledShowRecord[];
  skipped?: AutoGatherControlledShowRecord[];
  cumulativeBytes?: number;
  failedShow?: string;
  failedShowName?: string;
  failureReason?: string;
  startedAt?: string;
  endedAt?: string;
  message?: string;
  error?: string;
  sessionId?: string;
  lastRsyncPid?: number;
  lastSourceEntry?: string;
  rsyncProbe?: AutoGatherControlledRsyncProbe;
  requiresAcknowledgement?: boolean;
  canAcknowledge?: boolean;
  libraryRevision?: number;
  librarySummary?: AutoGatherLibrarySummary;
}

export interface AutoGatherLibrarySummary {
  revision: number;
  libraryPath?: string;
  showCount: number;
  splitCount: number;
  recommendationCount: number;
}

export interface AuthStatus {
  enabled: boolean;
  configured: boolean;
  authenticated: boolean;
  username: string;
  csrfToken: string;
}

export interface Unraid {
  numDisks: number;
  numProtected: number;
  synced: Date;
  syncErrs: number;
  resync: number;
  resyncPos: number;
  state: string;
  size: number;
  free: number;
  disks: Disk[];
  blockSize: number;
}

export interface Disk {
  id: number;
  name: string;
  path: string;
  device: string;
  type: string;
  fsType: string;
  free: number;
  size: number;
  serial: string;
  status: string;
  blocksTotal: number;
  blocksFree: number;
}

export enum CommandStatus {
  Complete = 0,
  Pending = 1,
  Flagged = 2,
  Stopped = 3,
  SourceRemoval = 4,
  InProgress = 5,
}

export interface Command {
  id: string;
  src: string;
  dst: string;
  entry: string;
  size: number;
  transferred: number;
  status: CommandStatus;
  reason: string;
}

export interface Operation {
  id: string;
  opKind: number;
  started: Date;
  finished: Date;
  bytesToTransfer: number;
  bytesTransferred: number;
  dryRun: boolean;
  rsyncArgs: string[];
  rsyncStrArgs: string;
  commands: Command[];
  completed: number;
  speed: number;
  remaining: string;
  deltaTransfer: number;
  line: string;
}

export interface History {
  version: number;
  lastChecked: Date;
  items: { [key: string]: Operation };
  order: string[];
}

export interface Item {
  name: string;
  size: number;
  path: string;
  location: string;
  blocksUsed: number;
}

export interface Bin {
  size: number;
  items: Item[];
  blocksUsed: number;
}

export interface VDisk {
  path: string;
  currentFree: number;
  plannedFree: number;
  bin: Bin;
  src: boolean;
  dst: boolean;
}

export interface Plan {
  id: string;
  started: Date;
  ended: Date;
  chosenFolders: string[];
  ownerIssue: number;
  groupIssue: number;
  folderIssue: number;
  fileIssue: number;
  vdisks: { [key: string]: VDisk };
  bytesToTransfer: number;
  target: string; // used for gather operations
}

export interface State {
  status: number;
  unraid: Unraid | null;
  operation: Operation | null;
  history: History | null;
  // plan: Plan | null;
}

export interface Node {
  id: string;
  label: string;
  leaf: boolean;
  dir: boolean;
  parent: string;
  checked?: boolean;
  expanded?: boolean;
  loading?: boolean;
  children: string[];
}

export type Nodes = Record<string, Node>;

export interface Icons {
  collapseIcon: React.ReactElement;
  expandIcon: React.ReactElement;
  checkedIcon: React.ReactElement;
  uncheckedIcon: React.ReactElement;
  parentIcon: React.ReactElement;
  leafIcon: React.ReactElement;
  hiddenIcon: React.ReactElement;
  loadingIcon: React.ReactElement;
}

export interface Branch {
  nodes: Nodes;
  order: string[];
}

export interface Sizes {
  disks: Record<string, number>;
  total: number;
}

export type Chosen = Record<string, boolean>;
export type Targets = Record<string, boolean>;

export enum Topic {
  CommandScatterPlanStart = 'scatter:plan:start',
  EventScatterPlanStarted = 'scatter:plan:started',
  EventScatterPlanProgress = 'scatter:plan:progress',
  EventScatterPlanEnded = 'scatter:plan:ended',
  CommandScatterMove = 'scatter:move',
  CommandScatterCopy = 'scatter:copy',
  CommandScatterValidate = 'scatter:validate',

  CommandGatherPlanStart = 'gather:plan:start',
  EventGatherPlanStarted = 'gather:plan:started',
  EventGatherPlanProgress = 'gather:plan:progress',
  EventGatherPlanEnded = 'gather:plan:ended',
  CommandGatherMove = 'gather:move',

  EventTransferStarted = 'transfer:started',
  EventTransferProgress = 'transfer:progress',
  EventTransferEnded = 'transfer:ended',

  EventOperationError = 'operation:error',

  CommandRemoveSource = 'remove:source',
  CommandReplay = 'replay',
  CommandStop = 'stop',
}

export interface Packet {
  topic: Topic;
  payload: unknown;
}

export enum ConfirmationKind {
  None = 0,
  Replay,
  ScatterValidate,
  RemoveSource,
}

export interface ConfirmationParams {
  kind: ConfirmationKind;
  operation?: Operation;
  command?: Command;
}
