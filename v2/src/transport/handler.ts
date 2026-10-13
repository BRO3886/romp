import type { JobService } from '../application/job-service.js';
import { assertNever } from '../shared/errors.js';
import { VERSION, type Request } from './protocol.js';

export function createHandler(service: JobService, stop: () => void) {
  return async (request: Request): Promise<unknown> => {
    switch (request.method) {
      case 'ping':
        return { version: VERSION };
      case 'run':
        return service.enqueue(request.params.root, request.params.issue);
      case 'watch':
        return service.watch(request.params.root, true);
      case 'unwatch':
        return service.watch(request.params.root, false);
      case 'state':
        await service.reconcile();
        return service.state();
      case 'job':
        return service.job(request.params.id);
      case 'wait':
        return service.wait(request.params.id);
      case 'logs':
        return service.logs(request.params.id);
      case 'cancel':
        await service.cancel(request.params.id);
        return { cancelled: true };
      case 'steer':
        return service.steer(request.params.id, request.params.content);
      case 'stop':
        stop();
        return { suspended: true };
      default:
        return assertNever(request);
    }
  };
}
