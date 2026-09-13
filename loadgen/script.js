import http from 'k6/http';
import { sleep } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30m',
    },
  },
};

// Hand-rolled W3C traceparent: k6/experimental/tracing was removed from k6
// (confirmed against the pinned grafana/k6:2.2.0 image — its replacement is
// a pure-JS jslib, not a built-in module, and pulling it in would add an
// external dependency this load generator doesn't otherwise have). This
// originates a fresh root trace per request so a full trace can be followed
// from k6 -> app -> Kafka -> consumer in Tempo.
function traceparent() {
  const hex = (n) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('');
  return `00-${hex(32)}-${hex(16)}-01`;
}

export default function () {
  const headers = { traceparent: traceparent() };
  const r = Math.random();
  if (r < 0.4) {
    http.get(`${BASE_URL}/users`, { headers });
  } else if (r < 0.7) {
    http.get(`${BASE_URL}/products`, { headers });
  } else if (r < 0.85) {
    http.post(`${BASE_URL}/orders`, JSON.stringify({ product_id: 1, qty: 1 }), {
      headers: { ...headers, 'Content-Type': 'application/json' },
    });
  } else if (r < 0.95) {
    http.get(`${BASE_URL}/slow`, { headers });
  } else {
    http.get(`${BASE_URL}/error`, { headers });
  }
  sleep(0.5);
}
