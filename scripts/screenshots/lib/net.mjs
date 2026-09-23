// Free-port picker.
//
// Neither `mobile-app` nor `serve` reports the port it actually bound: both
// read PORT (or --port) and hand that string straight to ListenAndServe/Run
// (cmd/mobile_app.go, cmd/serve.go). So there is nothing to read back, and the
// harness has to choose the port itself. Asking the kernel for one beats a
// hardcoded 81xx, which collides with whatever else is running locally.
import net from 'node:net';

export function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close((err) => (err ? reject(err) : resolve(port)));
    });
  });
}
