import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { StudioProvider } from "./ui";
import "antd/dist/reset.css";
import "./style.css";
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <StudioProvider>
      <App />
    </StudioProvider>
  </React.StrictMode>,
);
