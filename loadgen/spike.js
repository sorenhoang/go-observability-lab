import http from 'k6/http';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate',
      startRate: 20,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 300,
      stages: [
        { target: 20, duration: '1m' },
        { target: 250, duration: '30s' },
        { target: 250, duration: '1m' },
        { target: 20, duration: '30s' },
        { target: 20, duration: '1m' },
      ],
    },
  },
};

export default function () {
  http.get(`${BASE_URL}/users`);
}
