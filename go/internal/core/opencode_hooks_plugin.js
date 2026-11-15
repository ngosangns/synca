import { spawn } from "node:child_process";

const DATA = /* synca:data:begin */ null /* synca:data:end */;
const MAX_OUT = 65536;

function log(client, message) {
  const app = client && client.app;
  const fn = app && app.log;
  if (typeof fn !== "function") return;
  Promise.resolve(fn({ body: { service: "synca", level: "warn", message: String(message) } })).catch(function () {});
}

function matchesTool(hook, tool) {
  const names = hook && hook.toolNames ? hook.toolNames : [];
  if (names.length === 0) return true;
  return names.indexOf(tool) !== -1;
}

function stdinFor(hook, extra) {
  return JSON.stringify({
    hook_event_name: hook.event,
    session_id: extra.sessionID || "",
    cwd: extra.cwd || "",
    tool_name: extra.tool || "",
    tool_input: extra.toolInput || {},
    synca_tool_class: hook.classes && hook.classes.length ? hook.classes.join(",") : "",
  });
}

function take(buf, chunk) {
  if (buf.length >= MAX_OUT) return buf;
  return (buf + chunk.toString("utf8")).slice(0, MAX_OUT);
}

function runOne(hook, extra) {
  return new Promise(function (resolve, reject) {
    let settled = false;
    let timer = null;
    function done(err) {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      if (err) reject(err);
      else resolve();
    }
    let child;
    try {
      child = spawn("/bin/sh", ["-c", hook.command], {
        stdio: ["pipe", "pipe", "pipe"],
        env: Object.assign({}, process.env, {
          SYNCA_HOOK_ID: hook.id,
          SYNCA_AGENT: "opencode",
          SYNCA_PROJECT_DIR: extra.cwd || "",
        }),
      });
    } catch (err) {
      if (hook.block && hook.onFailure === "block") done(err);
      else done();
      return;
    }
    let stdout = "";
    let stderr = "";
    let killed = false;
    if (hook.timeoutSec > 0) {
      timer = setTimeout(function () {
        killed = true;
        child.kill("SIGKILL");
      }, hook.timeoutSec * 1000);
    }
    child.stdout.on("data", function (chunk) {
      stdout = take(stdout, chunk);
    });
    child.stderr.on("data", function (chunk) {
      stderr = take(stderr, chunk);
    });
    child.on("error", function (err) {
      if (hook.block && hook.onFailure === "block") done(err);
      else done();
    });
    child.on("close", function (code) {
      const hard = code === 2;
      const failed = killed || code !== 0;
      if (hook.block && (hard || (hook.onFailure === "block" && failed))) {
        done(new Error(stderr || "synca hook " + hook.id + " blocked"));
        return;
      }
      done();
    });
    try {
      child.stdin.end(stdinFor(hook, extra));
    } catch (err) {
      // The child may already have exited. stdout is drained so the pipe cannot block it.
      void stdout;
      void err;
    }
  });
}

async function runMatching(kind, pred, extra, client) {
  const hooks = (DATA && DATA.hooks) || [];
  for (let i = 0; i < hooks.length; i++) {
    const hook = hooks[i];
    if (!hook || hook.kind !== kind || !pred(hook)) continue;
    try {
      await runOne(hook, extra);
    } catch (err) {
      if (hook.block) throw err;
      log(client, err && err.message ? err.message : err);
    }
  }
}

export const SyncaHooks = async ({ client, directory }) => {
  const cwd = directory || "";
  return {
    "tool.execute.before": async (input, output) => {
      const tool = input && input.tool;
      await runMatching(
        "tool.execute.before",
        function (hook) {
          return matchesTool(hook, tool);
        },
        {
          sessionID: input && input.sessionID,
          cwd: cwd,
          tool: tool,
          toolInput: output && output.args,
        },
        client,
      );
    },
    "tool.execute.after": async (input) => {
      const tool = input && input.tool;
      await runMatching(
        "tool.execute.after",
        function (hook) {
          return matchesTool(hook, tool);
        },
        {
          sessionID: input && input.sessionID,
          cwd: cwd,
          tool: tool,
          toolInput: input && input.args,
        },
        client,
      );
    },
    event: async (input) => {
      const ev = input && input.event;
      const type = ev && ev.type;
      const props = (ev && ev.properties) || {};
      const info = props.info || {};
      await runMatching(
        "event",
        function (hook) {
          return hook.eventType === type;
        },
        {
          sessionID: props.sessionID || info.id || info.sessionID || "",
          cwd: cwd,
        },
        client,
      );
    },
    "chat.message": async (input) => {
      await runMatching(
        "chat.message",
        function () {
          return true;
        },
        { sessionID: input && input.sessionID, cwd: cwd },
        client,
      );
    },
    "experimental.session.compacting": async (input) => {
      await runMatching(
        "experimental.session.compacting",
        function () {
          return true;
        },
        { sessionID: input && input.sessionID, cwd: cwd },
        client,
      );
    },
  };
};
