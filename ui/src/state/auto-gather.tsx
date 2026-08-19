import { create } from 'zustand';
import { immer } from 'zustand/middleware/immer';

import { Api } from '~/api';
import { AutoGatherScanResult, AutoGatherShow } from '~/types';

interface AutoGatherStore {
  scanning: boolean;
  result: AutoGatherScanResult | null;
  error: string;
  actions: {
    scan: () => Promise<void>;
    applyResult: (result: AutoGatherScanResult) => void;
    clear: () => void;
  };
}

export const useAutoGatherStore = create<AutoGatherStore>()(
  immer((set) => ({
    scanning: false,
    result: null,
    error: '',
    actions: {
      scan: async () => {
        set((state) => {
          state.scanning = true;
          state.error = '';
        });

        try {
          const result = await Api.scanAutoGather();
          set((state) => {
            state.result = result;
            state.error = result.error || '';
            state.scanning = false;
          });
        } catch (e) {
          set((state) => {
            state.result = null;
            state.error = e instanceof Error ? e.message : 'Scan failed';
            state.scanning = false;
          });
        }
      },
      applyResult: (result) => {
        set((state) => {
          if (state.scanning) {
            return;
          }
          state.result = result;
          state.error = result.error || '';
        });
      },
      clear: () => {
        set((state) => {
          state.result = null;
          state.error = '';
          state.scanning = false;
        });
      },
    },
  })),
);

export const useAutoGatherActions = () =>
  useAutoGatherStore((state) => state.actions);
export const useAutoGatherScanning = () =>
  useAutoGatherStore((state) => state.scanning);
export const useAutoGatherResult = () =>
  useAutoGatherStore((state) => state.result);
export const useAutoGatherError = () =>
  useAutoGatherStore((state) => state.error);
export const useAutoGatherShows = (): AutoGatherShow[] =>
  useAutoGatherStore((state) => state.result?.shows ?? []);
