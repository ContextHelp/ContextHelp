import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider, createRouter } from '@tanstack/react-router';
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { routeTree } from './routeTree.gen';
import { capturePendingCode } from './auth-code';
import { isAuthRequired } from './api';
import './index.css';

// Before the router reads the location: lift a sign-in code out of the
// URL fragment and wipe it from the address bar and history.
capturePendingCode();

const queryClient: QueryClient = new QueryClient({
  // Any 401 means the session ended: re-ask who we are so the shell
  // swaps the page for the sign-in instructions.
  queryCache: new QueryCache({
    onError: (err, query) => {
      if (isAuthRequired(err) && query.queryKey[0] !== 'whoami') {
        void queryClient.invalidateQueries({ queryKey: ['whoami'] });
      }
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (count, err) => !isAuthRequired(err) && count < 1,
    },
  },
});

const router = createRouter({ routeTree, basepath: '/ui' });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>
);
