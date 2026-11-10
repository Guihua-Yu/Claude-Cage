import React, { useEffect, useRef, useState } from "react";
import { Check, ChevronLeft, LoaderCircle, ShieldCheck } from "lucide-react";
import { api, native, nativeAvailable } from "./api";
import { isRemoteClient } from "./client-context.mjs";
import { MacOSAccountGate, useAccount } from "./account";
import { GuestSystemPicker, HostCloneSummary, validGuestChoice } from "./guest-system-picker";
import { InitializationStorage } from "./initialization-storage";
import { ProxySettings } from "./proxy-settings";
import { SetupProgress } from "./setup-progress";
import { DiagnosticSettings } from "./diagnostics.jsx";
import { MacOSFirstSetup } from "./macos-first-setup";
import { InitializationProjectApproval } from "./macos-project-approval";
import { Modal } from "./modal";
function formatSpeed(bytesPerSecond) {
  if (!bytesPerSecond) return "—";
  if (bytesPerSecond >= 1 << 20) return `${(bytesPerSecond / (1 << 20)).toFixed(1)} MB/s`;
  return `${Math.round(bytesPerSecond / 1024)} KB/s`;
}
import { SafetyMessage } from "./safety-message";
import { needsProxySetup } from "./onboarding-state.mjs";
import { networkState } from "./safety-state.mjs";
import { useSSHNavigation } from "./remote-hosts";
import logo from "./logo.png";
import "./initialization.css";

const steps = [["system", "系统与存储"], ["proxy", "网络代理"], ["install", "安装与检查"]];

function InitializationFrame({ children }) {
  return <main className="initialization"><header className="initialization-brand"><img src={logo} alt="" /><strong>Cage</strong><span>首次设置</span></header>{children}</main>;
}

export function InitializationGate({ children }) {
  const { account } = useAccount();
  const [state, setState] = useState(null);
  const [error, setError] = useState("");
  const [attempt, retry] = useState(0);
  const remote = isRemoteClient();
  useEffect(() => {
    if (remote) return;
    let live = true;
    setError("");
    (async () => {
      const next = await api("/environment/initialization");
      if (next.required && !next.tracked && !next.started && !account?.initial_mac_setup) await api("/environment/initialization", { action: "begin" });
      if (live) setState(next);
    })().catch(error => { if (live) setError(error.message); });
    return () => { live = false; };
  }, [remote, attempt, account?.initial_mac_setup]);
  if (remote) return children;
  if (account?.initial_mac_setup) return <MacOSAccountGate><MacOSFirstSetup /></MacOSAccountGate>;
  if (error) return <InitializationFrame><section className="initialization-content"><h1>继续准备 Cage</h1><p role="alert"><SafetyMessage message={error} /></p><button className="primary" onClick={() => retry(value => value + 1)}>重新连接</button><DiagnosticSettings /></section></InitializationFrame>;
  if (!state) return <InitializationFrame><section className="initialization-content"><h1>正在读取环境状态</h1><p role="status"><LoaderCircle size={18} className="spin" />正在检查已有配置和安装进度…</p></section></InitializationFrame>;
  return state.required ? <MacOSAccountGate><Initialization initial={state} onComplete={() => setState({ required: false })} /></MacOSAccountGate> : children;
}

function Initialization({ initial, onComplete }) {
  const { account, refresh } = useAccount();
  const navigation = useSSHNavigation();
  const [step, setStep] = useState(initial.started || account?.guest_system ? "install" : "system");
  const [status, setStatus] = useState(null);
  const [proxy, setProxy] = useState(null);
  const [choice, setChoice] = useState(undefined);
  const [storage, setStorage] = useState(null);
  const [pending, setPending] = useState(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [connectionError, setConnectionError] = useState("");
  const [git, setGit] = useState(null);
  const [mirrors, setMirrors] = useState(null);
  const [mirrorChoice, setMirrorChoice] = useState("");
  const [details, setDetails] = useState(false);
  const [help, setHelp] = useState(false);
  const [dialog, setDialog] = useState(null);
  const [approvalWorkspace, setApprovalWorkspace] = useState("");
  const [proxyDirty, setProxyDirty] = useState(false);
  const alive = useRef(true);
  const pollNow = useRef(null);
  const activation = useRef(false);
  const actionPending = useRef(false);
  const restored = useRef(false);
  const job = status?.job || {};
  const busy = submitting || Boolean(job.running);
  const network = networkState(status);
  const needsProjectApproval = status?.guest_system === "macos" && status.project_errors?.some(item => item.path === status.environment_directory && item.recovery === "project-approval");
  const ready = Boolean(status?.ready && status.vm_running && !busy && !job.error && network.mode === "open");
  const confirm = options => new Promise(resolve => setDialog({ ...options, resolve }));
  const settleDialog = value => { dialog?.resolve(value); setDialog(null); };

  useEffect(() => {
    alive.current = true;
    let timer, polling = false;
    async function poll() {
      if (polling || !alive.current) return;
      clearTimeout(timer);
      polling = true;
      try {
        const next = await api("/status");
        if (!alive.current) return;
        setStatus(next);
        setConnectionError("");
        const results = await Promise.allSettled([
          api("/proxy"),
          account?.guest_system ? Promise.resolve(null) : api("/environment/guest-systems"),
        ]);
        if (!alive.current) return;
        if (results[0].status === "fulfilled") setProxy(results[0].value);
        else setConnectionError(results[0].reason.message);
        if (results[1].status === "fulfilled") {
          const install = results[1].value;
          setPending(install?.pending || null);
          if (!restored.current) {
            restored.current = true;
            if (initial.started && !initial.prepared && !account?.guest_system && !install?.pending && !next.job?.running && !next.job?.setup_progress && !next.ready) setStep("system");
          }
          if (install?.activate && !activation.current) {
            activation.current = true;
            setStep("install");
            try {
              await api("/environment/guest-systems/activate", {});
              await native("reconnect");
            } catch (error) {
              activation.current = false;
              if (alive.current) setError(error.message);
            }
          }
        } else setConnectionError(results[1].reason.message);
        if (next.job?.running && ["start", "guest-install", "safety"].includes(next.job.kind)) setStep("install");
      } catch (error) { if (alive.current) setConnectionError(error.message); }
      finally {
        polling = false;
        if (alive.current) timer = setTimeout(poll, 1200);
      }
    }
    pollNow.current = poll;
    poll();
    if (nativeAvailable()) native("workbenchReady").catch(() => {});
    window.cageWorkbenchAction = async action => { if (action === "quit") await native("quitNow"); };
    return () => { alive.current = false; clearTimeout(timer); pollNow.current = null; delete window.cageWorkbenchAction; };
  }, [account?.guest_system]);

  async function run(operation) {
    if (actionPending.current) return;
    actionPending.current = true;
    setSubmitting(true); setError("");
    try { await operation(); }
    catch (error) { if (alive.current) setError(error.message); }
    finally { actionPending.current = false; if (alive.current) { setSubmitting(false); pollNow.current?.(); } }
  }
  function startOptions() {
    if (account?.guest_system) return {};
    if (pending) return { guest_system: pending.system };
    if (!choice) return job.setup_progress?.system ? { guest_system: job.setup_progress.system } : {};
    return { guest_system: choice.guest_system, media: choice.media, sha256: choice.sha256, clone_key: choice.clone_key, editor_terms: choice.editor_terms };
  }
  function debianGuest() {
    const system = startOptions().guest_system || account?.guest_system || job.setup_progress?.system || "";
    return system.startsWith("debian");
  }
  async function install(skipMirrors) {
    if (needsProxySetup(null, status, proxy)) { setError("请先选择并应用代理节点。"); setStep("proxy"); return; }
    await run(async () => {
      const check = await api("/host/git");
      if (!check.ready) { setGit(check); return; }
      // Measure the Debian sources through the proxy once, before the first
      // install; the choice is kept for later updates and maintenance.
      if (skipMirrors !== true && debianGuest() && !status?.vm_running && !job.setup_progress) {
        const probe = await api("/host/debian-mirrors");
        if (!probe.decided) { setMirrors(probe); setMirrorChoice(probe.preference); return; }
      }
      await api("/environment/initialization", { action: "install" });
      const accepted = await api("/start", startOptions());
      if (!alive.current) return;
      setGit(null); setStep("install");
      setStatus(current => ({ ...current, job: { ...accepted, running: true, kind: accepted.kind || "start", message: accepted.message || "正在准备客机环境…" } }));
    });
  }
  async function chooseMirror() {
    await run(async () => { await api("/host/debian-mirrors", { preference: mirrorChoice }); setMirrors(null); });
    await install(true);
  }
  async function enableGit() {
    await run(async () => { const next = await api("/host/git", { confirm: true }); setGit(next.ready ? null : next); });
  }
  async function stop() {
    if (!await confirm({ title: "停止当前安装？", body: "安装进度、已下载文件和虚拟磁盘会保留。你可以在此界面继续安装。", confirm: "停止安装" })) return;
    await run(async () => { const accepted = await api("/stop", {}); setStatus(current => ({ ...current, job: { ...accepted, running: true } })); });
  }
  async function enter() {
    await run(async () => { await api("/environment/initialization", { action: "complete" }); await refresh(); if (alive.current) onComplete(); });
  }
  async function checkNetwork() {
    await run(async () => {
      const accepted = await api("/safety/check", {});
      setStatus(current => ({ ...current, job: { ...accepted, running: true, kind: "safety", message: "正在检查代理出口和环境隔离…" } }));
    });
  }
  async function back() {
    if (step === "proxy" && proxyDirty && !await confirm({ title: "放弃尚未应用的代理修改？", body: "已经保存并应用的代理会保留，当前未应用的表单修改会丢弃。", confirm: "放弃修改" })) return;
    setProxyDirty(false);
    setStep(step === "install" ? "proxy" : pending || account?.guest_system ? "install" : "system");
  }
  async function nextFromSystem() {
    await run(async () => {
      const directory = storage.directory.trim();
      if (directory !== storage.location.directory) {
        const accepted = await api("/environment/storage", { directory, previous: storage.location.directory, migrate: false });
        let job;
        do {
          await new Promise(resolve => setTimeout(resolve, 300));
          if (!alive.current) return;
          const next = await api("/status");
          setStatus(next); job = next.job;
          if (job.id !== accepted.id) throw new Error("存储操作状态已变化，请核对目录后重试。");
        } while (job.running);
        if (job.error) throw new Error(job.error);
        const location = await api("/environment/storage");
        if (location.directory !== directory) throw new Error("存储目录尚未保存，请重新选择后重试。");
        setStorage({ location, directory: location.directory });
      }
      setStep("proxy");
    });
  }
  const failed = Boolean(job.error || error);
  const networkPending = Boolean(status?.ready && status.vm_running && network.mode !== "open");
  const progress = job.setup_progress && { ...job, setup_progress: job.setup_progress };
  const title = step === "system" ? "为 Cage 准备独立环境" : step === "proxy" ? "连接你的网络代理" : ready ? "Cage 已准备就绪" : !busy && networkPending ? "环境已启动，等待联网检查" : failed ? "继续完成环境安装" : busy ? "正在准备你的环境" : "安装并检查客机环境";
  const index = steps.findIndex(([key]) => key === step);
  return <InitializationFrame>
    <section className="initialization-content">
      <ol className="initialization-steps" aria-label="初始化步骤">{steps.map(([key, label], position) => <li key={key} aria-current={step === key ? "step" : undefined}><span>{position < index ? <Check size={14} /> : position + 1}</span>{label}</li>)}</ol>
      <div className="initialization-title"><h1>{title}</h1><p>{step === "system" ? "选择客机系统和存储位置。初始化完成后，再进入工作台。" : step === "proxy" ? "这里的代理供客机内的进程和开发工具使用。系统镜像通过主机代理下载，遵循 Clash 等软件的系统代理或 TUN 设置。" : ready ? "系统、开发组件和网络检查均已完成，可以进入工作台开始使用。" : "下载、安装、必要授权和检查都在这里完成。中断后可以继续原任务。"}</p></div>
      {connectionError && <div className="initialization-notice" role="alert"><SafetyMessage message={connectionError} /><button onClick={() => pollNow.current?.()}>重新连接</button></div>}
      {!status ? <p role="status"><LoaderCircle size={18} className="spin" />正在读取安装状态…</p> : <>
        <div className="initialization-stage" key={step}>
          {step === "install" && needsProjectApproval && <section className="initialization-notice" aria-label="默认工作区访问">
            <p>虚拟 Mac 正在请求访问默认工作区。请在授权窗口中核对目录，再点击 Use Project。</p>
            <p className="macos-approval-path">{status.environment_directory}</p>
            <button type="button" disabled={!status.ready || !status.vm_running} onClick={() => setApprovalWorkspace(status.environment_directory)}>查看并授权</button>
          </section>}
          {step === "system" && <>
            <GuestSystemPicker system={choice} onChange={setChoice} preserveOnUnmount deferMatch />
            <InitializationStorage value={storage} onChange={setStorage} disabled={busy} />
          </>}
          {step === "proxy" && <><ProxySettings initializing status={status} onConfirm={confirm} onDirty={setProxyDirty} onApplied={async () => { setProxy(await api("/proxy")); pollNow.current?.(); }} />{choice?.clone && !needsProxySetup(null, status, proxy) && <HostCloneSummary pending={choice.pending} onChange={setChoice} />}</>}
          {step === "install" && (ready ? <div className="initialization-ready"><ShieldCheck size={36} /><h2>环境已就绪</h2><p>独立客机已安装，工具、文件和登录状态会持续保存。以后打开 Cage 可直接进入工作台。</p></div> : <>
            {progress ? <SetupProgress job={progress} onDetails={() => setDetails(true)} onStop={stop} onRetry={install} disconnected={Boolean(connectionError) || submitting} /> : <div className="initialization-operation" role="status">{busy && <LoaderCircle size={22} className="spin" />}<p>{job.message || (status.vm_running ? "请完成客机连接和网络检查。" : "准备继续原安装，已有文件和磁盘会保留。")}</p>{!busy && !job.error && <button className="primary" disabled={Boolean(connectionError)} onClick={install}>{status.ready ? "重新启动并检查" : "继续安装"}</button>}{busy && <button onClick={stop} disabled={submitting}>停止安装</button>}</div>}
            {job.error && !progress && <div className="initialization-notice" role="alert"><SafetyMessage message={job.error} />{!networkPending && <button className="primary" disabled={busy || Boolean(connectionError)} onClick={install}>继续安装</button>}</div>}
            {status.ready && network.mode !== "open" && !busy && <div className="initialization-network"><p>{status.network_error || status.safety?.error || "客机已经连接，请完成网络和隔离检查后进入工作台。"}</p><button className="primary" disabled={Boolean(connectionError)} onClick={checkNetwork}>运行网络与隔离检查</button></div>}
            {(progress?.setup_progress.phase === "image" || (failed && !networkPending)) && <p>系统镜像和在主机上准备的 macOS 安装组件遵循主机代理设置。下载失败时，可在操作详情中查看当前文件和重试记录。客机内的联网请求使用 Cage 代理。</p>}
            <div className="initialization-recovery"><button onClick={() => setStep("proxy")} disabled={busy}>检查 Cage 代理配置</button>{!progress && <button onClick={() => setDetails(true)}>查看操作详情</button>}</div>
          </>)}
        </div>
        {mirrors && <section className="initialization-notice" aria-label="Debian 软件源"><h2>安装前，选择 Debian 软件源</h2><p>已经所选代理分别测量各软件源的下载速度。国内直连时国内镜像通常更快；出口在海外时官方 CDN 更快。</p>
          <table className="mirror-samples"><tbody>{mirrors.samples.map(sample => <tr key={sample.id}><td>{sample.name}</td><td><code>{sample.host}</code></td><td>{sample.error ? <span className="mirror-error">{sample.error}</span> : formatSpeed(sample.bytes_per_second)}</td></tr>)}</tbody></table>
          <label className="mirror-option"><input type="radio" name="debian-mirror" value="auto" checked={mirrorChoice === "auto"} onChange={() => setMirrorChoice("auto")} />Debian 官方源优先（deb.debian.org CDN，不可达时回到国内镜像）{mirrors.recommended === "auto" && <em>推荐</em>}</label>
          <label className="mirror-option"><input type="radio" name="debian-mirror" value="domestic" checked={mirrorChoice === "domestic"} onChange={() => setMirrorChoice("domestic")} />国内镜像优先（南大、中科大，不可达时回到官方源）{mirrors.recommended === "domestic" && <em>推荐</em>}</label>
          <p>选择会保存在设置中，之后的安装、更新和维护沿用；可在客机的 <code>/etc/claude-cage/debian-mirror</code> 中改回。</p>
          <button className="primary" disabled={busy || !mirrorChoice} onClick={chooseMirror}>使用所选软件源并开始安装</button></section>}
        {git && <section className="initialization-notice" aria-label="Git 安全保护"><h2>安装前，保护本机 Git</h2><p>{git.message}</p>{git.can_enable && <><p>启用后，Git 会忽略未经你明确指定的裸仓库。该操作会修改当前用户的全局设置，影响本机其他 Git 工具，普通项目保持原有操作。</p><pre>git config --global safe.bareRepository explicit</pre><button className="primary" disabled={busy} onClick={enableGit}>启用 Git 保护</button></>}<button disabled={busy} onClick={install}>重新检查并安装</button></section>}
        {error && <p className="initialization-notice" role="alert"><SafetyMessage message={error} /></p>}
        <footer className="initialization-actions">
          {step === "system" ? <span /> : <button className="text-button" disabled={busy} onClick={back}><ChevronLeft size={16} />上一步</button>}
          {step === "system" && <button className="primary" disabled={busy || !storage?.directory.trim() || choice === undefined || (!choice?.clone && !validGuestChoice(choice)) || choice?.clone && choice.guest_system === "windows-11" && !choice.editor_terms || Boolean(connectionError)} onClick={nextFromSystem}>{submitting && <LoaderCircle size={16} className="spin" />}下一步：配置代理</button>}
          {step === "proxy" && <button className="primary" disabled={busy || proxyDirty || !account?.guest_system && !pending && !validGuestChoice(choice) || needsProxySetup(null, status, proxy) || Boolean(connectionError)} onClick={install}>{submitting ? <LoaderCircle size={16} className="spin" /> : null}开始安装</button>}
          {step === "install" && ready && <button className="primary" disabled={submitting || Boolean(connectionError)} onClick={enter}>进入工作台</button>}
        </footer>
      </>}
      <div className="initialization-support"><button className="text-button" aria-expanded={help} onClick={() => setHelp(!help)}>日志与诊断</button>{navigation && !busy && <button className="text-button" onClick={() => navigation.open()}>只连接 SSH 主机</button>}</div>
      {help && <DiagnosticSettings />}
    </section>
    {approvalWorkspace && <InitializationProjectApproval workspace={approvalWorkspace} jobRunning={Boolean(job.running)} onClose={() => { setApprovalWorkspace(""); pollNow.current?.(); }} />}
    {details && <Modal title="初始化操作详情" onClose={() => setDetails(false)}><div className="modal-body"><pre className="job-log">{job.log || job.message || "暂无操作记录"}</pre></div></Modal>}
    {dialog && <Modal title={dialog.title} onClose={() => settleDialog(false)} footer={<><button onClick={() => settleDialog(false)}>取消</button><button className="primary" onClick={() => settleDialog(true)}>{dialog.confirm}</button></>}><div className="modal-body"><p>{dialog.body}</p></div></Modal>}
  </InitializationFrame>;
}
