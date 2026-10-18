import { useCallback, useEffect, useRef, useState, type MouseEvent } from "react";
import { Window } from "@wailsio/runtime";
import { useChatService } from "./chat/useChatService";
import { Composer } from "./components/Composer";
import { Conversation } from "./components/Conversation";
import { WindowControls } from "./components/WindowControls";

const CONFIG_TEMPLATE = `{
  "endpoint": "https://api.example.com/v1/chat/completions",
  "apiKey": "sk-...",
  "model": "example-model",
  "includeUsage": false
}`;

const IS_MAC = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
const NEW_CHAT_SHORTCUT = IS_MAC ? "⌘ N" : "Ctrl N";
const NARROW_LAYOUT_BREAKPOINT = 720;
const RIGHT_SIDEBAR_BREAKPOINT = 820;

function ComposeIcon() {
    return (
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 20h9" />
            <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4L16.5 3.5Z" />
        </svg>
    );
}

function ChatIcon() {
    return (
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M20 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h9a4 4 0 0 1 4 4v8Z" />
        </svg>
    );
}

function SidebarIcon({ open }: { open: boolean }) {
    return (
        <svg viewBox="0 0 18 16" aria-hidden="true">
            <rect x="1.5" y="2" width="15" height="12" rx="1.8" />
            <path d="M6.8 2v12" />
            {open && <path className="sidebar-icon-panel" d="M3.2 3.5h2.1v9H3.2z" />}
        </svg>
    );
}

function RightSidebarIcon({ open }: { open: boolean }) {
    return (
        <svg viewBox="0 0 18 16" aria-hidden="true">
            <rect x="1.5" y="2" width="15" height="12" rx="1.8" />
            <path d="M11.2 2v12" />
            {open && <path className="sidebar-icon-panel" d="M12.7 3.5h2.1v9h-2.1z" />}
        </svg>
    );
}

function BottomPanelIcon({ open }: { open: boolean }) {
    return (
        <svg viewBox="0 0 18 16" aria-hidden="true">
            <rect x="1.5" y="2" width="15" height="12" rx="1.8" />
            <path d="M1.5 10.2h15" />
            {open && <path className="sidebar-icon-panel" d="M3.2 11.4h11.6v1.4H3.2z" />}
        </svg>
    );
}

function App() {
    const [prompt, setPrompt] = useState("");
    const [bottomPanelOpen, setBottomPanelOpen] = useState(true);
    const [leftSidebarOpen, setLeftSidebarOpen] = useState(
        () => window.innerWidth > NARROW_LAYOUT_BREAKPOINT,
    );
    const [rightSidebarOpen, setRightSidebarOpen] = useState(
        () => window.innerWidth > RIGHT_SIDEBAR_BREAKPOINT,
    );
    const narrowLayout = useRef(window.innerWidth <= NARROW_LAYOUT_BREAKPOINT);
    const compactLayout = useRef(window.innerWidth <= RIGHT_SIDEBAR_BREAKPOINT);
    const { status, messages, running, problem, send, stop, clear, reload } = useChatService();
    const configured = status?.configured ?? false;

    // 配置有问题、或者 Status 这个调用本身失败，都要给用户看到原因：
    // 否则界面只剩一个灰掉的输入框。
    const showConfig = !configured && (status !== null || problem !== null);

    const handleSend = useCallback(async () => {
        if (await send(prompt)) {
            setPrompt("");
        }
    }, [prompt, send]);

    const handleNew = useCallback(() => {
        clear();
        setPrompt("");
        // WebKit 有时会让 CodeMirror 保留焦点，点「新对话」后仍在输入框里闪光标。
        // 新会话应回到中性状态，等用户真正点击输入框时再重新获得焦点。
        if (document.activeElement instanceof HTMLElement) {
            document.activeElement.blur();
        }
        if (window.innerWidth <= NARROW_LAYOUT_BREAKPOINT) {
            setLeftSidebarOpen(false);
        }
    }, [clear]);

    const toggleLeftSidebar = useCallback(() => {
        const opening = !leftSidebarOpen;
        if (opening && window.innerWidth <= NARROW_LAYOUT_BREAKPOINT) {
            setRightSidebarOpen(false);
        }
        setLeftSidebarOpen(opening);
    }, [leftSidebarOpen]);

    const toggleRightSidebar = useCallback(() => {
        const opening = !rightSidebarOpen;
        if (opening && window.innerWidth <= RIGHT_SIDEBAR_BREAKPOINT) {
            setLeftSidebarOpen(false);
        }
        setRightSidebarOpen(opening);
    }, [rightSidebarOpen]);

    const toggleBottomPanel = useCallback(() => {
        setBottomPanelOpen((open) => !open);
    }, []);

    // 失去系统标题栏后，双击顶部最大化是最自然的预期动作。
    // 点在按钮上时不要触发：那是按钮自己的点击。
    const handleTitleDoubleClick = useCallback((event: MouseEvent<HTMLElement>) => {
        if ((event.target as HTMLElement).closest("button") !== null) {
            return;
        }

        void Window.ToggleMaximise().catch(() => undefined);
    }, []);

    // 只在跨越断点时重置侧栏状态，避免窗口连续缩放时反复覆盖用户选择。
    useEffect(() => {
        const handleResize = () => {
            const nextNarrow = window.innerWidth <= NARROW_LAYOUT_BREAKPOINT;
            if (nextNarrow !== narrowLayout.current) {
                narrowLayout.current = nextNarrow;
                setLeftSidebarOpen(!nextNarrow);
            }

            const nextCompact = window.innerWidth <= RIGHT_SIDEBAR_BREAKPOINT;
            if (nextCompact !== compactLayout.current) {
                compactLayout.current = nextCompact;
                setRightSidebarOpen(!nextCompact);
            }
        };

        window.addEventListener("resize", handleResize);

        return () => window.removeEventListener("resize", handleResize);
    }, []);

    useEffect(() => {
        const handleKeyDown = (event: KeyboardEvent) => {
            if (
                event.key === "Escape" &&
                rightSidebarOpen &&
                window.innerWidth <= RIGHT_SIDEBAR_BREAKPOINT
            ) {
                event.preventDefault();
                setRightSidebarOpen(false);
                return;
            }

            if (
                event.key === "Escape" &&
                leftSidebarOpen &&
                window.innerWidth <= NARROW_LAYOUT_BREAKPOINT
            ) {
                event.preventDefault();
                setLeftSidebarOpen(false);
                return;
            }

            const newChat =
                (event.metaKey || event.ctrlKey) &&
                !event.shiftKey &&
                !event.altKey &&
                event.key.toLowerCase() === "n";

            if (newChat) {
                event.preventDefault();
                handleNew();
                return;
            }

            const toggleBottomPanelShortcut =
                (event.metaKey || event.ctrlKey) &&
                !event.shiftKey &&
                !event.altKey &&
                event.key.toLowerCase() === "j";

            if (toggleBottomPanelShortcut) {
                event.preventDefault();
                toggleBottomPanel();
                return;
            }

            const toggleRightSidebarShortcut =
                (event.metaKey || event.ctrlKey) &&
                event.altKey &&
                !event.shiftKey &&
                event.key.toLowerCase() === "b";

            if (toggleRightSidebarShortcut) {
                event.preventDefault();
                toggleRightSidebar();
                return;
            }

            const toggleLeftSidebarShortcut =
                (event.metaKey || event.ctrlKey) &&
                !event.shiftKey &&
                !event.altKey &&
                event.key.toLowerCase() === "b";

            if (toggleLeftSidebarShortcut) {
                event.preventDefault();
                toggleLeftSidebar();
            }
        };

        window.addEventListener("keydown", handleKeyDown);

        return () => window.removeEventListener("keydown", handleKeyDown);
    }, [
        handleNew,
        leftSidebarOpen,
        rightSidebarOpen,
        toggleBottomPanel,
        toggleLeftSidebar,
        toggleRightSidebar,
    ]);

    return (
        <div
            className={[
                "app-shell",
                leftSidebarOpen ? "" : "is-left-sidebar-collapsed",
                rightSidebarOpen ? "" : "is-right-sidebar-collapsed",
            ]
                .filter(Boolean)
                .join(" ")}
        >
            <header className="app-topbar" onDoubleClick={handleTitleDoubleClick}>
                <div className="topbar-actions">
                    <button
                        className="topbar-toggle"
                        type="button"
                        onClick={toggleLeftSidebar}
                        aria-label={leftSidebarOpen ? "收起左侧边栏" : "展开左侧边栏"}
                        aria-pressed={leftSidebarOpen}
                        title={`${leftSidebarOpen ? "收起" : "展开"}左侧边栏 (${IS_MAC ? "⌘" : "Ctrl"} B)`}
                    >
                        <SidebarIcon open={leftSidebarOpen} />
                    </button>
                    <button
                        className="topbar-toggle"
                        type="button"
                        onClick={toggleBottomPanel}
                        aria-label={bottomPanelOpen ? "收起底部面板" : "展开底部面板"}
                        aria-pressed={bottomPanelOpen}
                        title={`${bottomPanelOpen ? "收起" : "展开"}底部面板 (${IS_MAC ? "⌘" : "Ctrl"} J)`}
                    >
                        <BottomPanelIcon open={bottomPanelOpen} />
                    </button>
                    <button
                        className="topbar-toggle"
                        type="button"
                        onClick={toggleRightSidebar}
                        aria-label={rightSidebarOpen ? "收起右侧边栏" : "展开右侧边栏"}
                        aria-pressed={rightSidebarOpen}
                        title={`${rightSidebarOpen ? "收起" : "展开"}右侧边栏 (${IS_MAC ? "⌘⌥B" : "Ctrl+Alt+B"})`}
                    >
                        <RightSidebarIcon open={rightSidebarOpen} />
                    </button>
                    <WindowControls />
                </div>
            </header>

            <div className="app-body">
                <aside className="sidebar">
                    <div className="brand-lockup">
                        <span className="brand-symbol" aria-hidden="true">
                            <span />
                        </span>
                        <span>neutron star</span>
                    </div>

                    <button className="new-chat" type="button" onClick={handleNew} disabled={running}>
                        <ComposeIcon />
                        新对话
                        <kbd>{NEW_CHAT_SHORTCUT}</kbd>
                    </button>

                    <nav className="thread-list" aria-label="会话">
                        <p>最近</p>
                        <button className="thread is-active" type="button">
                            <ChatIcon />
                            <span>
                                <strong>{messages.length > 0 ? "当前对话" : "新的对话"}</strong>
                                <small>{running ? "正在生成" : "本地工作区"}</small>
                            </span>
                        </button>
                    </nav>

                    <div className="sidebar-spacer" />

                    <div className="connection-card">
                        <span className={`connection-dot${configured ? " is-online" : ""}`} />
                        <div>
                            <strong>{configured ? status?.model || "模型已连接" : "模型未连接"}</strong>
                            <span>{configured ? "Chat Completions" : "等待配置"}</span>
                        </div>
                    </div>
                </aside>

                <main className="workspace">
                    {showConfig && (
                        <section className="config-banner">
                            <div>
                                <span className="config-label">连接设置</span>
                                <h3>{status?.problem || problem || "尚未配置模型连接"}</h3>
                                <p>
                                    编辑 <code>{status?.configPath || "用户配置目录/model.json"}</code>
                                    ，保存后点「重新加载」。
                                </p>
                                <button className="config-reload" type="button" onClick={reload}>
                                    重新加载配置
                                </button>
                            </div>
                            <pre>{CONFIG_TEMPLATE}</pre>
                        </section>
                    )}

                    <Conversation messages={messages} configured={configured} />

                    {problem && configured && messages.length === 0 && (
                        <div className="global-notice" role="alert">
                            {problem}
                        </div>
                    )}

                    <Composer
                        value={prompt}
                        onChange={setPrompt}
                        onSubmit={() => void handleSend()}
                        onStop={stop}
                        running={running}
                        disabled={!configured}
                    />

                    <section
                        className={`bottom-panel${bottomPanelOpen ? "" : " is-collapsed"}`}
                        aria-label="底部面板"
                        aria-hidden={!bottomPanelOpen}
                    >
                        <div className="bottom-panel-body" />
                    </section>
                </main>

                <aside
                    className="right-sidebar"
                    aria-label="右侧边栏"
                    aria-hidden={!rightSidebarOpen}
                />
            </div>
        </div>
    );
}

export default App;
