import "./i18n";
import * as ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import "./main.css";
import App from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
    <BrowserRouter>
        <App />
    </BrowserRouter>
);
