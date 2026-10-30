import { createBrowserRouter } from 'react-router';
import { WaAccountInfoRoute, WaCreateAccountRoute, WaHomeRoute, WaInboxRoute, WaLayout, WaNotFoundRoute, WaReloginAccountRoute } from './wa-page';

export const waRouter = createBrowserRouter([
  {
    path: '/',
    Component: WaLayout,
    children: [
      { index: true, Component: WaHomeRoute },
      { path: 'accounts/new', Component: WaCreateAccountRoute },
      {
        path: 'accounts/:accountID',
        children: [
          { index: true, Component: WaAccountInfoRoute },
          { path: 'login', Component: WaReloginAccountRoute },
          { path: 'chats', Component: WaInboxRoute },
          { path: 'chats/:contactID', Component: WaInboxRoute },
        ],
      },
      { path: '*', Component: WaNotFoundRoute },
    ],
  },
]);
