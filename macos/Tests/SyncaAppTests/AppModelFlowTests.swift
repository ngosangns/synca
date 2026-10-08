import Foundation
import Testing
@testable import synca_app
@testable import SyncaKit

// Helpers -------------------------------------------------------------------

@MainActor private func loaded(_ m: AppModel) async -> Bool {
    await m.reload().value
    let ready = m.inventory == .ready || m.inventory == .needsProject
    return ready && !m.isBusy
}

@MainActor private func plan(_ m: AppModel) -> Plan? {
    if case .plan(let p) = m.route { return p }
    return nil
}

@MainActor private func settle(_ m: AppModel) async {
    _ = await eventually { !m.isBusy }
}

private func link(_ sbx: Sandbox, _ rel: String) -> String? {
    try? FileManager.default.destinationOfSymbolicLink(atPath: sbx.home.appendingPathComponent(rel).path)
}

// Inventory / filter / selection ----------------------------------------------

@MainActor @Suite(.serialized) struct InventoryTests {
    @Test func loadsSkillsAndMcpsAndFilters() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha", description: "git helper")
        try sbx.skill("beta", description: "database tool")
        try sbx.cursorMcp(["srv-one": ["command": "npx", "args": ["-y", "p"]]])
        let m = sbx.makeModel()
        #expect(m.inventory == .idle)
        #expect(await loaded(m))
        #expect(m.skills.map(\.key) == ["alpha", "beta"])
        #expect(m.mcps.map(\.key) == ["srv-one"])
        m.filter = "database"
        #expect(m.filteredSkills.map(\.key) == ["beta"])   // matches description
        #expect(m.filteredMcps.isEmpty)
        m.filter = "SRV"
        #expect(m.filteredMcps.map(\.key) == ["srv-one"])  // case-insensitive
    }

    @Test func selectionResolvesAndIsPrunedWhenItemDisappears() async throws {
        let sbx = try Sandbox()
        let dir = try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.selection = .skill("alpha")
        #expect(m.selectedSkill?.key == "alpha" && m.selectedMcp == nil)
        try FileManager.default.removeItem(at: dir)
        #expect(await loaded(m))
        #expect(m.selection == nil && m.selectedSkill == nil)
    }

    @Test func emptyHomeIsReadyNotFailed() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        #expect(m.inventory == .ready && m.skills.isEmpty && m.mcps.isEmpty)
    }

    @Test func selectingAnItemLeavesAFlowRoute() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.route = .installSkill
        m.selection = .skill("alpha")
        guard case .item = m.route else { Issue.record("route should reset to .item"); return }
    }

    @Test func skillFileTreeMatchesDisk() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha", extra: ["references/a.md": "x", "scripts/run.sh": "echo"])
        let m = sbx.makeModel()
        #expect(await loaded(m))
        let path = try #require(m.skills.first?.primaryPath)
        let nodes = FileTree.load(root: URL(fileURLWithPath: path))
        #expect(nodes.map(\.name) == ["references", "scripts", "SKILL.md"])
        let skillMd = try #require(nodes.first { $0.name == "SKILL.md" })
        #expect(FileTree.preview(of: skillMd.url)?.contains("name: alpha") == true)
    }
}

// Scope & project manager ------------------------------------------------------

@MainActor @Suite(.serialized) struct ProjectTests {
    @Test func projectScopeNeedsAProjectThenLoadsIt() async throws {
        let sbx = try Sandbox()
        try sbx.skill("user-skill")
        try sbx.skill("proj-skill", in: sbx.project.appendingPathComponent(".agents/skills"))
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.scope = .project
        #expect(await eventually { m.inventory == .needsProject })
        m.addProjects([sbx.project.path])
        #expect(m.projectDir == ProjectList.normalize(sbx.project.path))
        #expect(await eventually { m.inventory == .ready })
        #expect(m.skills.map(\.key).contains("proj-skill"))
        #expect(!m.skills.map(\.key).contains("user-skill"))
        m.scope = .user
        #expect(await eventually { m.skills.map(\.key).contains("user-skill") })
    }

    @Test func multipleProjectsSwitchRemoveAndPersist() async throws {
        let sbx = try Sandbox()
        let p2 = sbx.project.deletingLastPathComponent().appendingPathComponent("proj2")
        try Sandbox.makeRepo(p2)
        try sbx.skill("only-in-1", in: sbx.project.appendingPathComponent(".agents/skills"))
        try sbx.skill("only-in-2", in: p2.appendingPathComponent(".agents/skills"))

        let m = sbx.makeModel()
        m.scope = .project
        m.addProjects([sbx.project.path, p2.path])
        #expect(m.projects.paths.count == 2 && m.projects.active == ProjectList.normalize(p2.path))
        #expect(await eventually { m.skills.map(\.key) == ["only-in-2"] })

        m.selectProject(sbx.project.path)
        #expect(await eventually { m.skills.map(\.key) == ["only-in-1"] })

        // persisted across a new model instance
        let reopened = sbx.makeModel()
        #expect(reopened.projects == m.projects)

        m.removeProject(sbx.project.path)       // active -> falls to neighbour
        #expect(m.projects.paths == [ProjectList.normalize(p2.path)])
        #expect(await eventually { m.skills.map(\.key) == ["only-in-2"] })
        m.removeProject(p2.path)
        #expect(m.projectDir == nil)
        #expect(await eventually { m.inventory == .needsProject })
        #expect(sbx.makeModel().projects.paths.isEmpty)
    }

    @Test func switchingProjectWhileInUserScopeDoesNotReload() async throws {
        let sbx = try Sandbox()
        try sbx.skill("u")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.addProjects([sbx.project.path])
        #expect(m.scope == .user && m.skills.map(\.key) == ["u"])
    }
}

// Sync ---------------------------------------------------------------------------

@MainActor @Suite(.serialized) struct SyncTests {
    @Test func planThenApplyLinksEveryAgent() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(await loaded(m))

        m.planSync(target: .all)
        #expect(m.isBusy)                                   // loading awareness
        #expect(await eventually { plan(m) != nil })
        let p = try #require(plan(m))
        #expect(p.ok && p.conflicts.isEmpty)
        #expect(p.chips.contains { $0.kind == "symlink_skill" })
        #expect(!sbx.exists(".cursor/skills/alpha"), "dry run must not touch disk")

        m.apply(p)
        await settle(m)
        #expect(await eventually { m.toast?.style == .success })
        guard case .item = m.route else { Issue.record("route should return to .item"); return }
        #expect(link(sbx, ".cursor/skills/alpha")?.hasSuffix(".agents/skills/alpha") == true)
        #expect(link(sbx, ".claude/skills/alpha") != nil)
        #expect(await eventually { m.skills.first?.presence.count ?? 0 > 1 })
    }

    @Test func focusedSyncOnlyTouchesThatSkill() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha"); try sbx.skill("beta")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .skills, key: "alpha")
        #expect(await eventually { plan(m) != nil })
        m.apply(try #require(plan(m)))
        await settle(m)
        #expect(link(sbx, ".cursor/skills/alpha") != nil)
        #expect(link(sbx, ".cursor/skills/beta") == nil)
    }

    @Test func conflictIsSkippedByDefaultAndResolvedWhenChosen() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha", description: "canonical")
        // different content in another agent's real directory => conflict
        try sbx.skill("alpha", in: sbx.home.appendingPathComponent(".cursor/skills"), description: "DIFFERENT")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        #expect(m.skills.first?.mismatch == true)

        m.planSync(target: .skills)
        #expect(await eventually { plan(m) != nil })
        var p = try #require(plan(m))
        #expect(p.conflicts.map(\.key) == ["alpha"] && p.conflicts[0].policy == .skip)

        // 1) default = skip: nothing overwritten
        m.apply(p); await settle(m)
        let cursorMd = sbx.home.appendingPathComponent(".cursor/skills/alpha/SKILL.md")
        #expect(try String(contentsOf: cursorMd, encoding: .utf8).contains("DIFFERENT"))
        #expect(link(sbx, ".cursor/skills/alpha") == nil, "real dir must stay a real dir")

        // 2) keep-source: cursor copy replaced by link to canonical
        m.planSync(target: .skills)
        #expect(await eventually { plan(m) != nil })
        p = try #require(plan(m))
        p.conflicts[0].policy = .keepSource
        m.apply(p); await settle(m)
        #expect(await eventually { link(sbx, ".cursor/skills/alpha") != nil })
        #expect(await eventually { m.skills.first?.mismatch == false })
    }

    @Test func conflictPlanCarriesBothCopiesAndTheyDiffAgainstDisk() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha", description: "canonical", extra: ["refs/a.md": "same", "only-src.txt": "s"])
        try sbx.skill("alpha", in: sbx.home.appendingPathComponent(".cursor/skills"), description: "DIFFERENT",
                      extra: ["refs/a.md": "same", "only-tgt.txt": "t"])
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .skills)
        #expect(await eventually { plan(m) != nil })
        let c = try #require(plan(m)?.conflicts.first)
        #expect(c.paths.count == 2 && c.hashes.count == 2 && c.hashes[0] != c.hashes[1])

        let cmp = try #require(ConflictCompare.skill(c, entry: m.skills.first))
        #expect(cmp.source.agent == "agents" && cmp.target.agent == "cursor")
        #expect(cmp.files.map { "\($0.path):\($0.status)" } == [
            "SKILL.md:modified", "only-tgt.txt:added", "only-src.txt:removed", "refs/a.md:identical"])
        guard case .text(let rows) = cmp.files[0].content else { Issue.record("SKILL.md should diff as text"); return }
        #expect(rows.contains { $0.kind == .removed && $0.text.contains("canonical") })
        #expect(rows.contains { $0.kind == .added && $0.text.contains("DIFFERENT") })
    }

    @Test func keepTargetKeepsTheDifferingCopyEvenWhenAnEarlierAgentIsASymlink() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha", description: "CANON")
        let grok = sbx.home.appendingPathComponent(".grok/skills")
        try FileManager.default.createDirectory(at: grok, withIntermediateDirectories: true)
        try FileManager.default.createSymbolicLink(atPath: grok.appendingPathComponent("alpha").path,
                                                   withDestinationPath: "../../.agents/skills/alpha")
        try sbx.skill("alpha", in: sbx.home.appendingPathComponent(".cursor/skills"), description: "CURSOR-VERSION")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .skills)
        #expect(await eventually { plan(m) != nil })
        var p = try #require(plan(m))
        let cmp = try #require(ConflictCompare.skill(p.conflicts[0], entry: m.skills.first))
        #expect(cmp.target.agent == "cursor", "the UI must show the copy keep-target will actually keep")
        p.conflicts[0].policy = .keepTarget
        m.apply(p); await settle(m)
        let md = sbx.home.appendingPathComponent(".agents/skills/alpha/SKILL.md")
        #expect(try String(contentsOf: md, encoding: .utf8).contains("CURSOR-VERSION"))
    }

    @Test func mcpConflictShowsConfigDiff() async throws {
        let sbx = try Sandbox()
        let hub = sbx.home.appendingPathComponent(".agents/mcp.json")
        try FileManager.default.createDirectory(at: hub.deletingLastPathComponent(), withIntermediateDirectories: true)
        try #"{"mcpServers":{"srv":{"command":"npx","args":["-y","a"]}}}"#.write(to: hub, atomically: true, encoding: .utf8)
        try sbx.cursorMcp(["srv": ["command": "npx", "args": ["-y", "b"]]])
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .mcp)
        #expect(await eventually { plan(m) != nil })
        let c = try #require(plan(m)?.conflicts.first)
        #expect(c.kind == .mcp && c.fingerprints.count >= 2)
        let cmp = try #require(ConflictCompare.mcp(c, entry: m.mcps.first))
        guard case .text(let rows) = cmp.files[0].content else { Issue.record("expected text diff"); return }
        #expect(rows.filter { $0.kind == .removed }.map(\.text) == ["  a"])
        #expect(rows.filter { $0.kind == .added }.map(\.text) == ["  b"])
    }

    @Test func nothingToDoPlanIsOkWithoutConflicts() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .all)
        #expect(await eventually { plan(m) != nil })
        #expect(plan(m)?.ok == true && plan(m)?.conflicts.isEmpty == true)
    }
}

// Install / add / remove ---------------------------------------------------------

@MainActor @Suite(.serialized) struct ManageTests {
    @Test func installSkillPreviewThenApply() async throws {
        let sbx = try Sandbox()
        let src = try sbx.skill("fresh", in: sbx.project.deletingLastPathComponent().appendingPathComponent("src"))
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.previewInstallSkill(source: src.path)
        #expect(await eventually { plan(m) != nil })
        let p = try #require(plan(m))
        #expect(p.ok && p.chips.contains { $0.kind == "copy_skill" })
        #expect(!sbx.exists(".agents/skills/fresh"), "preview must not install")
        m.apply(p); await settle(m)
        #expect(sbx.exists(".agents/skills/fresh/SKILL.md"))
        #expect(await eventually { m.skills.map(\.key) == ["fresh"] })
        #expect(m.toast?.style == .success)
    }

    @Test func installFromMissingPathReportsFailure() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.previewInstallSkill(source: "/definitely/not/here")
        #expect(await eventually { plan(m) != nil })
        let p = try #require(plan(m))
        #expect(!p.ok, "failed preview must disable Apply")
        #expect(!p.output.isEmpty)
    }

    @Test func addMcpPreviewApplyThenRemove() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        let payload = AddMcpPayload(name: "srv", transport: "stdio", command: "npx -y pkg", url: nil, enabled: true)
        m.previewAddMcp(payload)
        #expect(await eventually { plan(m) != nil })
        let p = try #require(plan(m))
        #expect(p.ok && !sbx.exists(".agents/mcp.json"), "preview must not write")
        m.apply(p); await settle(m)
        #expect(sbx.exists(".agents/mcp.json") && sbx.exists(".cursor/mcp.json"))
        #expect(await eventually { m.mcps.map(\.key) == ["srv"] })
        let cfg = try #require(m.mcps.first?.presence.first?.normalized)
        #expect(cfg.command == ["npx"] && cfg.args == ["-y", "pkg"])

        m.selection = .mcp("srv")
        m.confirmRemoveMcp("srv")
        let c = try #require(m.confirmation)
        #expect(c.destructive)
        c.action(); await settle(m)
        #expect(await eventually { m.mcps.isEmpty })
        #expect(m.selection == nil)
    }

    @Test func addHttpMcpWithUrl() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.previewAddMcp(AddMcpPayload(name: "remote", transport: "http", command: nil,
                                      url: "https://example.com/mcp", enabled: false))
        #expect(await eventually { plan(m) != nil })
        m.apply(try #require(plan(m))); await settle(m)
        #expect(await eventually { m.mcps.first?.presence.first?.normalized?.url == "https://example.com/mcp" })
    }

    @Test func unlinkKeepsCanonicalPurgeRemovesEverything() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha"); try sbx.skill("beta")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .all)
        #expect(await eventually { plan(m) != nil })
        m.apply(try #require(plan(m))); await settle(m)
        #expect(link(sbx, ".cursor/skills/alpha") != nil)

        // Unlink: agent links gone, canonical kept
        m.confirmRemoveSkill("alpha", purge: false)
        let unlink = try #require(m.confirmation)
        #expect(unlink.title.contains("Unlink") && unlink.destructive)
        unlink.action(); await settle(m)
        #expect(link(sbx, ".cursor/skills/alpha") == nil)
        #expect(sbx.exists(".agents/skills/alpha/SKILL.md"))

        // Purge: canonical gone too, selection cleared
        m.selection = .skill("beta")
        m.confirmRemoveSkill("beta", purge: true)
        let purge = try #require(m.confirmation)
        #expect(purge.title.contains("Purge") && purge.message.contains("cannot be undone"))
        purge.action(); await settle(m)
        #expect(!sbx.exists(".agents/skills/beta") && link(sbx, ".cursor/skills/beta") == nil)
        #expect(await eventually { m.selection == nil && !m.skills.map(\.key).contains("beta") })
        #expect(m.skills.map(\.key).contains("alpha"))
    }

    @Test func purgeWorksWhenFolderNameDiffersFromSkillName() async throws {
        let sbx = try Sandbox()
        // folder "gitbutler", SKILL.md name "but" => key "but"
        let dir = sbx.home.appendingPathComponent(".agents/skills/gitbutler")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try "---\nname: but\ndescription: d\n---\n".write(to: dir.appendingPathComponent("SKILL.md"), atomically: true, encoding: .utf8)
        let cursor = sbx.home.appendingPathComponent(".cursor/skills")
        try FileManager.default.createDirectory(at: cursor, withIntermediateDirectories: true)
        try FileManager.default.createSymbolicLink(atPath: cursor.appendingPathComponent("gitbutler").path,
                                                   withDestinationPath: "../../.agents/skills/gitbutler")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        #expect(m.skills.map(\.key) == ["but"])
        m.selection = .skill("but")
        m.confirmRemoveSkill("but", purge: true)
        m.confirmation?.action(); await settle(m)
        #expect(await eventually { m.skills.isEmpty })
        #expect(!sbx.exists(".agents/skills/gitbutler") && link(sbx, ".cursor/skills/gitbutler") == nil)
        #expect(m.toast?.style == .success && m.selection == nil)
    }

    @Test func removingSomethingThatIsNotThereIsAnErrorNotSuccess() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.confirmRemoveSkill("ghost", purge: true)
        m.confirmation?.action(); await settle(m)
        #expect(await eventually { m.toast?.style == .error })
        #expect(m.toast?.message.contains("Nothing was removed") == true)
    }

    @Test func confirmationDoesNothingUntilConfirmed() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.confirmRemoveSkill("alpha", purge: true)
        #expect(m.confirmation != nil)
        m.confirmation = nil                      // user cancels
        await settle(m)
        #expect(sbx.exists(".agents/skills/alpha/SKILL.md"))
        #expect(m.activity.isEmpty, "cancelling must not run the CLI")
    }

}

// Update, feedback, activity, errors ---------------------------------------------

@MainActor @Suite(.serialized) struct FeedbackTests {
    @Test func updateCheckEndsInAResultOrFailureState() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        m.checkUpdate()
        guard case .update(.checking) = m.route else { Issue.record("should show checking state"); return }
        #expect(m.isBusy)
        #expect(await eventually(timeout: .seconds(60)) {
            if case .update(.checking) = m.route { return false }
            return true
        })
        switch m.route {
        case .update(.result(let info)): #expect(info.current != nil || info.message != nil)
        case .update(.failed(let msg)): #expect(!msg.isEmpty)
        default: Issue.record("unexpected route \(m.route)")
        }
        #expect(!m.isBusy)
    }

    @Test func missingCLIIsReportedNotCrashed() async throws {
        let sbx = try Sandbox()
        let bad = SyncaCLI(executable: URL(fileURLWithPath: "/nonexistent/synca"))
        let m = AppModel(cli: bad, log: LogStore(directory: sbx.logDir), defaults: sbx.defaults)
        m.start()
        #expect(await eventually { m.cliChecked })
        #expect(m.cliMissing && m.cliVersion == nil)
        #expect(await eventually { if case .failed = m.inventory { return true } else { return false } })
        m.planSync(target: .all)
        #expect(m.toast?.style == .error)
        guard case .item = m.route else { Issue.record("route must not get stuck"); return }
    }

    @Test func startReportsCLIVersion() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        m.start()
        #expect(await eventually { m.cliVersion?.contains("synca") == true })
        #expect(!m.cliMissing)
    }

    @Test func toastAutoDismissesAndCanBeDismissed() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        m.notify("hello", .info)
        #expect(m.toast?.message == "hello")
        m.dismissToast()
        #expect(m.toast == nil)
        m.notify("a", .success); let first = m.toast?.id
        m.notify("b", .error)
        #expect(m.toast?.message == "b" && m.toast?.id != first)
    }

    @Test func activityLogsEveryCommandNewestFirstAndPages() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.planSync(target: .all)
        #expect(await eventually { plan(m) != nil })
        m.apply(try #require(plan(m))); await settle(m)
        let cmds = m.activity.map(\.command)
        #expect(cmds.count == 2)                         // list calls are not logged: dry-run + apply
        #expect(cmds[0].contains("--on-conflict skip") && cmds[1].contains("--dry-run"))
        #expect(m.activity.first { $0.command.contains("--dry-run") }?.status == "dry-run")
        #expect(m.activity.allSatisfy { $0.exit == 0 })
        #expect(zip(m.activity, m.activity.dropFirst()).allSatisfy { $0.id > $1.id })

        // survives restart through LogStore
        let again = sbx.makeModel()
        again.loadMoreActivity(reset: true)
        #expect(await eventually { again.activity.count == m.activity.count })

        m.clearActivity()
        #expect(await eventually { m.activity.isEmpty })
        again.loadMoreActivity(reset: true)
        #expect(await eventually { !again.activityLoading })
        #expect(again.activity.isEmpty)
    }

    @Test func failedCommandsAreLoggedAsErrors() async throws {
        let sbx = try Sandbox()
        let m = sbx.makeModel()
        #expect(await loaded(m))
        m.previewInstallSkill(source: "/nope")
        #expect(await eventually { plan(m) != nil })
        let entry = try #require(m.activity.first)
        #expect(entry.status == "error" && entry.exit != 0)
    }

    @Test func operationsDriveBusyStateAndAlwaysClear() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        #expect(!m.isBusy)
        let t = m.reload()
        #expect(m.isBusy && m.operations.last?.title.contains("inventory") == true)
        await t.value
        #expect(!m.isBusy)
        #expect(!m.isRefreshing)
        m.planSync(target: .all)
        #expect(m.isBusy && m.operations.last?.title.contains("sync") == true)
        #expect(await eventually { plan(m) != nil })
        #expect(await eventually { !m.isBusy })
    }

    @Test func rapidReloadsKeepLatestAndNeverStickBusy() async throws {
        let sbx = try Sandbox()
        try sbx.skill("alpha")
        let m = sbx.makeModel()
        var last: Task<Void, Never>?
        for _ in 0..<8 { last = m.reload() }
        await last?.value
        #expect(m.inventory == .ready)
        #expect(await eventually { !m.isBusy && !m.isRefreshing })
        #expect(m.skills.map(\.key) == ["alpha"])
    }
}
