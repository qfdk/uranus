// ip.tar.tn：以纯文本返回访问者的公网 IP。Uranus 首次启动时用它确定服务器的 IP。
// CF-Connecting-IP 由 Cloudflare 填写，访客自己带上的会被覆盖，伪造不了。
export default {
  fetch(request) {
    if (request.method !== 'GET' && request.method !== 'HEAD') {
      return reply(405, 'method not allowed', {Allow: 'GET, HEAD'});
    }
    if (new URL(request.url).pathname !== '/') {
      return reply(404, 'not found');
    }
    const ip = request.headers.get('CF-Connecting-IP');
    if (!ip) {
      return reply(500, 'unknown client address');
    }
    return reply(200, ip);
  },
};

function reply(status, text, headers = {}) {
  return new Response(text + '\n', {
    status,
    headers: {'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store', ...headers},
  });
}
