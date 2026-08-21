import { Op } from '~/types';

export function getRouteFromStatus(status: Op): string {
  switch (status) {
    case Op.Neutral:
      return '/scatter/select';
    case Op.ScatterPlan:
      return '/scatter/plan';
    case Op.GatherPlan:
      return '/gather/plan';
    case Op.ScatterMove:
    case Op.ScatterCopy:
    case Op.ScatterValidate:
      return '/scatter/transfer/operation';
    case Op.GatherMove:
      return '/gather/transfer/operation';
    case Op.AutoGatherDryRun:
    case Op.AutoGatherReal:
      return '/auto-gather';
    default:
      return '/scatter/select';
  }
}

export const routeToStep = (route: string): number => {
  switch (route) {
    case '/':
    case '/scatter':
    case '/scatter/select':
    case '/gather':
    case '/gather/select':
      return 1;
    case '/scatter/plan':
    case '/gather/plan':
      return 2;
    case '/scatter/transfer/validation':
    case '/scatter/transfer/operation':
    case '/gather/transfer/targets':
    case '/gather/transfer/operation':
      return 3;
    default:
      return 1;
  }
};

export const getNextRoute = (route: string) => {
  switch (route) {
    case '/':
      return '/scatter/select';
    case '/scatter':
      return '/scatter/select';
    case '/scatter/select':
      return '/scatter/plan';
    case '/scatter/plan':
      return '/scatter/transfer/validation';
    case '/scatter/transfer/validation':
      return '/scatter/transfer/operation';
    case '/gather':
      return '/gather/select';
    case '/gather/select':
      return '/gather/plan';
    case '/gather/plan':
      return '/gather/transfer';
    case '/history':
      return '/history';
    case '/settings':
      return '/settings';
    case '/logs':
      return '/logs';
    default:
      return '/scatter/select';
  }
};

export const getBaseRoute = (path: string) => {
  const parts = path.split('/');
  const firstLevel = parts.find((part) => part !== '');
  return '/' + firstLevel;
};

export function canonicalAppPath(pathname?: string | null): string | null {
  if (!pathname) {
    return null;
  }
  const path = pathname.split('?')[0].replace(/\/+$/, '') || '/';
  if (path === '/log' || path.startsWith('/log/')) {
    return '/logs';
  }
  if (path === '/logs' || path.startsWith('/logs/')) {
    return '/logs';
  }
  if (path === '/auto-gather' || path.startsWith('/auto-gather/')) {
    return '/auto-gather';
  }
  if (path === '/history' || path.startsWith('/history/')) {
    return '/history';
  }
  if (path === '/settings') {
    return '/settings';
  }
  if (path.startsWith('/settings/')) {
    return path;
  }
  if (path === '/scatter') {
    return '/scatter/select';
  }
  if (path.startsWith('/scatter/')) {
    return path;
  }
  if (path === '/gather') {
    return '/gather/select';
  }
  if (path.startsWith('/gather/')) {
    return path;
  }
  if (path === '/login') {
    return '/login';
  }
  if (path === '/') {
    return '/scatter/select';
  }
  return null;
}
