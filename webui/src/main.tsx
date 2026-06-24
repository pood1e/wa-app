import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from 'next-themes';
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router/dom';
import { TooltipProvider } from '@/components/ui/tooltip';
import { waRouter } from './dashboard/wa-routes';
import './styles.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { refetchOnWindowFocus: false, retry: 1, staleTime: 5000 },
  },
});

createRoot(document.getElementById('root')!).render(
  <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange storageKey="wa-app-theme">
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={waRouter} />
      </TooltipProvider>
    </QueryClientProvider>
  </ThemeProvider>,
);
