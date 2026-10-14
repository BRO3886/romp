import { DurableObject } from 'cloudflare:workers';
import { CloudSandbox } from '../../src/sandbox.js';
export class SandboxProbe extends DurableObject {
  override async fetch() {
    const sandbox = new CloudSandbox(this.ctx);
    try {
      const first = await sandbox.exec([
        'node',
        '-e',
        'require("fs").writeFileSync("/workspace/receipt.txt","42");process.stdout.write(process.version)',
      ]);
      if (first.exitCode !== 0) throw new Error('Execution failed');
      const git = await sandbox.exec([
        'bash',
        '-lc',
        'git init -b main /workspace/repo && cd /workspace/repo && printf 0 > answer.txt && git add . && git commit -m "test: seed" && git worktree add -b test /workspace/job',
      ]);
      if (git.exitCode) throw new Error(git.stderr);
      await sandbox.checkpoint();
      await sandbox.destroy();
      const restored = new CloudSandbox(this.ctx);
      const result = await restored.exec(['cat', '/workspace/receipt.txt']);
      const status = await restored.exec(['git', 'status', '--porcelain'], {
        cwd: '/workspace/job',
      });
      if (status.exitCode || status.stdout) throw new Error('Git worktree restore failed');
      await restored.destroy();
      if (result.stdout !== '42') throw new Error('Snapshot restore failed');
      return Response.json({ node: first.stdout, restored: result.stdout });
    } catch (error) {
      await sandbox.destroy();
      return Response.json(
        { error: error instanceof Error ? error.message : 'Failed' },
        { status: 500 },
      );
    }
  }
}
export default {
  fetch(_request: Request, env: { PROBE: DurableObjectNamespace<SandboxProbe> }) {
    return env.PROBE.getByName('probe').fetch('https://probe');
  },
};
