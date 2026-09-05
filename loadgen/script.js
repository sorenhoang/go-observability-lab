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

export default function () {
  const r = Math.random();
  if (r < 0.4) {
    http.get(`${BASE_URL}/users`);
  } else if (r < 0.7) {
    http.get(`${BASE_URL}/products`);
  } else if (r < 0.85) {
    http.post(`${BASE_URL}/orders`, JSON.stringify({ product_id: 1, qty: 1 }), {
      headers: { 'Content-Type': 'application/json' },
    });
  } else if (r < 0.95) {
    http.get(`${BASE_URL}/slow`);
  } else {
    http.get(`${BASE_URL}/error`);
  }
  sleep(0.5);
}
