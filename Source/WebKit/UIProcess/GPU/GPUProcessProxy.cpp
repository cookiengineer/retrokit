/*
 * Copyright (C) 2019-2024 Apple Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY APPLE INC. AND ITS CONTRIBUTORS ``AS IS''
 * AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
 * THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
 * PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL APPLE INC. OR ITS CONTRIBUTORS
 * BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
 * CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
 * SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
 * INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
 * CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF
 * THE POSSIBILITY OF SUCH DAMAGE.
 */

#include "config.h"
#include "GPUProcessProxy.h"

#if ENABLE(GPU_PROCESS)

#include "DrawingAreaProxy.h"
#include "GPUProcessConnectionParameters.h"
#include "GPUProcessCreationParameters.h"
#include "GPUProcessMessages.h"
#include "GPUProcessPreferences.h"
#include "GPUProcessProxyMessages.h"
#include "GPUProcessSessionParameters.h"
#include "Logging.h"
#include "OverrideLanguages.h"
#include "ProcessTerminationReason.h"
#include "ProvisionalPageProxy.h"
#include "SharedFileHandle.h"
#include "WebKitServiceNames.h"
#include "WebPageGroup.h"
#include "WebPageMessages.h"
#include "WebPageProxy.h"
#include "WebPreferences.h"
#include "WebProcessMessages.h"
#include "WebProcessPool.h"
#include "WebProcessProxy.h"
#include "WebProcessProxyMessages.h"
#include <WebCore/LogInitialization.h>
#include <WebCore/ScreenProperties.h>
#include <algorithm>
#include <wtf/CompletionHandler.h>
#include <wtf/LogInitialization.h>
#include <wtf/MachSendRight.h>
#include <wtf/TZoneMallocInlines.h>
#include <wtf/TranslatedProcess.h>

#if PLATFORM(IOS_FAMILY)
#include <wtf/spi/darwin/XPCSPI.h>
#endif

#if USE(SANDBOX_EXTENSIONS_FOR_CACHE_AND_TEMP_DIRECTORY_ACCESS)
#include "SandboxUtilities.h"
#include <wtf/FileSystem.h>
#endif

#if PLATFORM(COCOA)
#include <wtf/BlockPtr.h>
#include <wtf/cocoa/RuntimeApplicationChecksCocoa.h>
#endif

#if USE(GBM)
#include "DRMMainDevice.h"
#endif

#if ENABLE(EXTENSION_CAPABILITIES)
#include "ExtensionCapabilityGrant.h"
#include "ExtensionCapabilityGranter.h"
#include "MediaCapability.h"
#endif

#if HAVE(POWERLOG_TASK_MODE_QUERY)
#include <wtf/darwin/DispatchExtras.h>
#endif

namespace WebKit {
using namespace WebCore;


static WeakPtr<GPUProcessProxy>& NODELETE singleton()
{
    static NeverDestroyed<WeakPtr<GPUProcessProxy>> singleton;
    return singleton;
}

static RefPtr<GPUProcessProxy>& NODELETE keptAliveGPUProcessProxy()
{
    static MainRunLoopNeverDestroyed<RefPtr<GPUProcessProxy>> keptAliveGPUProcessProxy;
    return keptAliveGPUProcessProxy.get();
}

WTF_MAKE_TZONE_ALLOCATED_IMPL(GPUProcessProxy);

void GPUProcessProxy::keepProcessAliveTemporarily()
{
    ASSERT(isMainRunLoop());
    static constexpr auto durationToKeepGPUProcessAliveAfterDestruction = 1_min;

    if (!singleton())
        return;

    keptAliveGPUProcessProxy() = singleton().get();
    static NeverDestroyed<RunLoop::Timer> releaseGPUProcessTimer(RunLoop::mainSingleton(), "GPUProcessProxy::KeepProcessAliveTemporarily::ReleaseGPUProcessTimer"_s, [] {
        keptAliveGPUProcessProxy() = nullptr;
    });
    releaseGPUProcessTimer.get().startOneShot(durationToKeepGPUProcessAliveAfterDestruction);
}

Ref<GPUProcessProxy> GPUProcessProxy::getOrCreate()
{
    ASSERT(RunLoop::isMain());
    if (auto& existingGPUProcess = singleton()) {
        ASSERT(existingGPUProcess->state() != State::Terminated);
        return *existingGPUProcess;
    }
    Ref gpuProcess = adoptRef(*new GPUProcessProxy);
    singleton() = gpuProcess;
    return gpuProcess;
}

GPUProcessProxy* GPUProcessProxy::singletonIfCreated()
{
    return singleton().get();
}

#if USE(SANDBOX_EXTENSIONS_FOR_CACHE_AND_TEMP_DIRECTORY_ACCESS)
static String gpuProcessCachesDirectory()
{
    constexpr ASCIILiteral cacheDirectory = "/Library/Caches/com.apple.WebKit.GPU/"_s;

    String path = WebsiteDataStore::cacheDirectoryInContainerOrHomeDirectory(cacheDirectory);

    FileSystem::makeAllDirectories(path);
    
    return path;
}
#endif

GPUProcessProxy::GPUProcessProxy()
    : AuxiliaryProcessProxy("GPUProcess"_s, WebProcessPool::anyProcessPoolNeedsUIBackgroundAssertion() ? ShouldTakeUIBackgroundAssertion::Yes : ShouldTakeUIBackgroundAssertion::No)
{
    connect();

    GPUProcessCreationParameters parameters;
    parameters.auxiliaryProcessParameters = auxiliaryProcessParameters();
    parameters.overrideLanguages = overrideLanguages();


#if USE(SANDBOX_EXTENSIONS_FOR_CACHE_AND_TEMP_DIRECTORY_ACCESS)
    parameters.containerCachesDirectory = resolveAndCreateReadWriteDirectoryForSandboxExtension(gpuProcessCachesDirectory());
    auto containerTemporaryDirectory = WebsiteDataStore::defaultResolvedContainerTemporaryDirectory();

    if (!parameters.containerCachesDirectory.isEmpty()) {
        if (auto handle = SandboxExtension::createHandleWithoutResolvingPath(parameters.containerCachesDirectory, SandboxExtension::Type::ReadWrite))
            parameters.containerCachesDirectoryExtensionHandle = WTF::move(*handle);
    }

    if (!containerTemporaryDirectory.isEmpty()) {
        auto tempDirectory = resolveAndCreateReadWriteDirectoryForSandboxExtension(FileSystem::pathByAppendingComponent(containerTemporaryDirectory, gpuServiceName));
        if (auto handle = SandboxExtension::createHandleWithoutResolvingPath(tempDirectory, SandboxExtension::Type::ReadWrite))
            parameters.containerTemporaryDirectoryExtensionHandle = WTF::move(*handle);
    }
#endif

#if USE(GBM)
    parameters.drmDevice = drmMainDevice();
#endif

#if PLATFORM(COCOA)
    m_isMetalDebugDeviceEnabledForTesting = s_enableMetalDebugDeviceInNewGPUProcessesForTesting;
    m_isMetalShaderValidationEnabledForTesting = s_enableMetalShaderValidationInNewGPUProcessesForTesting;
    if (s_gpuProcessMediaCodecCapabilities) {
#if ENABLE(VP9)
        parameters.hasVP9HardwareDecoder = s_gpuProcessMediaCodecCapabilities->hasVP9HardwareDecoder;
#endif
#if ENABLE(AV1)
        parameters.hasAV1HardwareDecoder = s_gpuProcessMediaCodecCapabilities->hasAV1HardwareDecoder;
#endif
    }
#endif

    platformInitializeGPUProcessParameters(parameters);

#if PLATFORM(COCOA)
    m_hasSentGPUToolsSandboxExtensions = !parameters.gpuToolsExtensionHandles.isEmpty();
#endif

    // Initialize the GPU process.
    sendWithAsyncReply(Messages::GPUProcess::InitializeGPUProcess(WTF::move(parameters)), [initializationActivityAndGrant = initializationActivityAndGrant()] { });

#if HAVE(AUDIO_COMPONENT_SERVER_REGISTRATIONS) && ENABLE(AUDIO_COMPONENT_SERVER_REGISTRATIONS_IN_GPU_PROCESS)
    auto registrations = fetchAudioComponentServerRegistrations();
    RELEASE_ASSERT(registrations);
    send(Messages::GPUProcess::ConsumeAudioComponentRegistrations(IPC::SharedBufferReference(WTF::move(registrations))), 0);
#endif

    updateProcessAssertion();
}

GPUProcessProxy::~GPUProcessProxy() = default;


#if HAVE(SCREEN_CAPTURE_KIT)
void GPUProcessProxy::promptForGetDisplayMedia(WebCore::DisplayCapturePromptType type, CompletionHandler<void(std::optional<WebCore::CaptureDevice>)>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::PromptForGetDisplayMedia { type }, WTF::move(completionHandler));
}

void GPUProcessProxy::cancelGetDisplayMediaPrompt()
{
    send(Messages::GPUProcess::CancelGetDisplayMediaPrompt { }, 0);
}
#endif

void GPUProcessProxy::getLaunchOptions(ProcessLauncher::LaunchOptions& launchOptions)
{
    launchOptions.processType = ProcessLauncher::ProcessType::GPU;
    AuxiliaryProcessProxy::getLaunchOptions(launchOptions);
}

void GPUProcessProxy::connectionWillOpen(IPC::Connection&)
{
    RELEASE_LOG(Process, "%p - GPUProcessProxy::connectionWillOpen:", this);
}

void GPUProcessProxy::processWillShutDown(IPC::Connection& connection)
{
    RELEASE_LOG(Process, "%p - GPUProcessProxy::processWillShutDown:", this);
    ASSERT_UNUSED(connection, &this->connection() == &connection);
    if (singleton() == this)
        singleton() = nullptr;
}

std::optional<GPUProcessMediaCodecCapabilities> GPUProcessProxy::s_gpuProcessMediaCodecCapabilities;

void GPUProcessProxy::createGPUProcessConnection(WebProcessProxy& webProcessProxy, IPC::Connection::Handle&& connectionIdentifier, GPUProcessConnectionParameters&& parameters)
{
    parameters.mediaCodecCapabilities = s_gpuProcessMediaCodecCapabilities;

    if (RefPtr store = webProcessProxy.websiteDataStore())
        addSession(*store);

    RELEASE_LOG(ProcessSuspension, "%p - GPUProcessProxy is taking a background assertion because a web process is requesting a connection", this);
    startResponsivenessTimer(UseLazyStop::No);
    sendWithAsyncReply(Messages::GPUProcess::CreateGPUConnectionToWebProcess { webProcessProxy.coreProcessIdentifier(), webProcessProxy.sessionID(), WTF::move(connectionIdentifier), WTF::move(parameters) }, [weakThis = WeakPtr { *this }]() mutable {
        if (RefPtr protectedThis = weakThis.get())
            protectedThis->stopResponsivenessTimer();
    }, 0, IPC::SendOption::DispatchMessageEvenWhenWaitingForSyncReply);
}

void GPUProcessProxy::sharedPreferencesForWebProcessDidChange(WebProcessProxy& webProcessProxy, SharedPreferencesForWebProcess&& sharedPreferencesForWebProcess, CompletionHandler<void()>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::SharedPreferencesForWebProcessDidChange { webProcessProxy.coreProcessIdentifier(), WTF::move(sharedPreferencesForWebProcess) }, WTF::move(completionHandler));
}

void GPUProcessProxy::gpuProcessExited(ProcessTerminationReason reason)
{
    Ref protectedThis { *this };

    switch (reason) {
    case ProcessTerminationReason::ExceededMemoryLimit:
    case ProcessTerminationReason::ExceededCPULimit:
    case ProcessTerminationReason::RequestedByClient:
    case ProcessTerminationReason::IdleExit:
    case ProcessTerminationReason::Unresponsive:
    case ProcessTerminationReason::Crash:
        RELEASE_LOG_ERROR(Process, "%p - GPUProcessProxy::gpuProcessExited: reason=%" PUBLIC_LOG_STRING, this, processTerminationReasonToString(reason).characters());
        break;
    case ProcessTerminationReason::ExceededProcessCountLimit:
    case ProcessTerminationReason::NavigationSwap:
    case ProcessTerminationReason::RequestedByNetworkProcess:
    case ProcessTerminationReason::RequestedByGPUProcess:
    case ProcessTerminationReason::RequestedByModelProcess:
    case ProcessTerminationReason::GPUProcessCrashedTooManyTimes:
    case ProcessTerminationReason::ModelProcessCrashedTooManyTimes:
    case ProcessTerminationReason::NonMainFrameWebContentProcessCrash:
        ASSERT_NOT_REACHED();
        break;
    }

#if ENABLE(EXTENSION_CAPABILITIES)
    ExtensionCapabilityGranter::invalidateGrants(moveToVector(std::exchange(extensionCapabilityGrants(), { }).values()));
#endif

    if (keptAliveGPUProcessProxy() == this)
        keptAliveGPUProcessProxy() = nullptr;
    if (singleton() == this)
        singleton() = nullptr;

    for (auto& processPool : WebProcessPool::allProcessPools())
        processPool->gpuProcessExited(processID(), reason);
}

void GPUProcessProxy::processIsReadyToExit()
{
    RELEASE_LOG(Process, "%p - GPUProcessProxy::processIsReadyToExit:", this);
    terminate();
    gpuProcessExited(ProcessTerminationReason::IdleExit); // May cause |this| to get deleted.
}

void GPUProcessProxy::childConnectionDidBecomeUnresponsive()
{
    RELEASE_LOG_ERROR(Process, "%p - GPUProcessProxy::childConnectionDidBecomeUnresponsive:", this);
    didBecomeUnresponsive();
}

void GPUProcessProxy::terminateForTesting()
{
    processIsReadyToExit();
}

void GPUProcessProxy::webProcessConnectionCountForTesting(CompletionHandler<void(uint64_t)>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::WebProcessConnectionCountForTesting(), WTF::move(completionHandler));
}

void GPUProcessProxy::didClose(IPC::Connection&)
{
    RELEASE_LOG_ERROR(Process, "%p - GPUProcessProxy::didClose:", this);
    gpuProcessExited(ProcessTerminationReason::Crash); // May cause |this| to get deleted.
}

void GPUProcessProxy::didReceiveInvalidMessage(IPC::Connection& connection, IPC::MessageName messageName, const Vector<uint32_t>&)
{
    logInvalidMessage(connection, messageName);

    WebProcessPool::didReceiveInvalidMessage(messageName);

    // Terminate the GPU process.
    terminate();

    // Since we've invalidated the connection we'll never get a IPC::Connection::Client::didClose
    // callback so we'll explicitly call it here instead.
    didClose(connection);
}

void GPUProcessProxy::didFinishLaunching(ProcessLauncher* launcher, IPC::Connection::Identifier&& connectionIdentifier)
{
    RELEASE_LOG(Process, "%p - GPUProcessProxy::didFinishLaunching:", this);

    bool didTerminate = !connectionIdentifier;

    AuxiliaryProcessProxy::didFinishLaunching(launcher, WTF::move(connectionIdentifier));

    if (didTerminate) {
        gpuProcessExited(ProcessTerminationReason::Crash);
        return;
    }

#if PLATFORM(COCOA)
    if (auto networkProcess = NetworkProcessProxy::defaultNetworkProcess())
        networkProcess->sendXPCEndpointToProcess(*this);
#endif

    beginResponsivenessChecks();

    for (auto& processPool : WebProcessPool::allProcessPools())
        processPool->gpuProcessDidFinishLaunching(processID());

#if HAVE(POWERLOG_TASK_MODE_QUERY)
    dispatch_async(globalDispatchQueueSingleton(DISPATCH_QUEUE_PRIORITY_DEFAULT, 0), makeBlockPtr([weakThis = WeakPtr { *this }] () mutable {
        if (!isPowerLoggingInTaskMode())
            return;
        RunLoop::mainSingleton().dispatch([weakThis = WTF::move(weakThis)] () {
            if (!weakThis)
                return;
            weakThis->enablePowerLogging();
        });
    }).get());
#endif

#if USE(EXTENSIONKIT)
    sendBookmarkDataForCacheDirectory();
#endif
}



void GPUProcessProxy::updateProcessAssertion()
{
    bool hasAnyForegroundWebProcesses = false;
    bool hasAnyBackgroundWebProcesses = false;

    for (auto& processPool : WebProcessPool::allProcessPools()) {
        hasAnyForegroundWebProcesses |= processPool->hasForegroundWebProcesses();
        hasAnyBackgroundWebProcesses |= processPool->hasBackgroundWebProcesses();
    }

    if (hasAnyForegroundWebProcesses) {
        if (!ProcessThrottler::isValidForegroundActivity(m_activityFromWebProcesses.get())) {
            m_activityFromWebProcesses = protect(throttler())->foregroundActivity("GPU for foreground view(s)"_s);
        }
        return;
    }
    if (hasAnyBackgroundWebProcesses) {
        if (!ProcessThrottler::isValidBackgroundActivity(m_activityFromWebProcesses.get())) {
            m_activityFromWebProcesses = protect(throttler())->backgroundActivity("GPU for background view(s)"_s);
        }
        return;
    }

    // Use std::exchange() instead of a simple nullptr assignment to avoid re-entering this
    // function during the destructor of the ProcessThrottler activity, before setting
    // m_activityFromWebProcesses.
    std::exchange(m_activityFromWebProcesses, nullptr);
}

static inline GPUProcessSessionParameters gpuProcessSessionParameters(const WebsiteDataStore& store)
{
    GPUProcessSessionParameters parameters;

    auto& resolvedDirectories = const_cast<WebsiteDataStore&>(store).resolvedDirectories();
    parameters.mediaCacheDirectory = resolvedDirectories.mediaCacheDirectory;
    SandboxExtension::Handle mediaCacheDirectoryExtensionHandle;
    if (!parameters.mediaCacheDirectory.isEmpty()) {
        if (auto handle = SandboxExtension::createHandleWithoutResolvingPath(parameters.mediaCacheDirectory, SandboxExtension::Type::ReadWrite))
            parameters.mediaCacheDirectorySandboxExtensionHandle = WTF::move(*handle);
    }

#if ENABLE(LEGACY_ENCRYPTED_MEDIA) || ENABLE(ENCRYPTED_MEDIA)
    parameters.mediaKeysStorageDirectory = resolvedDirectories.mediaKeysStorageDirectory;
    SandboxExtension::Handle mediaKeysStorageDirectorySandboxExtensionHandle;
    if (!parameters.mediaKeysStorageDirectory.isEmpty()) {
        if (auto handle = SandboxExtension::createHandleWithoutResolvingPath(parameters.mediaKeysStorageDirectory, SandboxExtension::Type::ReadWrite))
            parameters.mediaKeysStorageDirectorySandboxExtensionHandle = WTF::move(*handle);
    }
#endif

    return parameters;
}

void GPUProcessProxy::addSession(const WebsiteDataStore& store)
{
    if (!canSendMessage())
        return;

    if (m_sessionIDs.contains(store.sessionID()))
        return;

    send(Messages::GPUProcess::AddSession { store.sessionID(), gpuProcessSessionParameters(store) }, 0);
    m_sessionIDs.add(store.sessionID());
}

void GPUProcessProxy::removeSession(PAL::SessionID sessionID)
{
    if (!canSendMessage())
        return;

    if (m_sessionIDs.remove(sessionID))
        send(Messages::GPUProcess::RemoveSession { sessionID }, 0);
}

void GPUProcessProxy::sendPrepareToSuspend(IsSuspensionImminent isSuspensionImminent, double remainingRunTime, CompletionHandler<void()>&& completionHandler)
{
    RELEASE_LOG(ProcessSuspension, "%p - GPUProcessProxy::sendPrepareToSuspend:", this);
    sendWithAsyncReply(Messages::GPUProcess::PrepareToSuspend(isSuspensionImminent == IsSuspensionImminent::Yes, MonotonicTime::now() + Seconds(remainingRunTime)), WTF::move(completionHandler), 0, { }, ShouldStartProcessThrottlerActivity::No);
}

void GPUProcessProxy::sendProcessDidResume(ResumeReason)
{
    RELEASE_LOG(ProcessSuspension, "%p - GPUProcessProxy::sendProcessDidResume:", this);
    if (canSendMessage())
        send(Messages::GPUProcess::ProcessDidResume(), 0);
}

void GPUProcessProxy::terminateWebProcess(WebCore::ProcessIdentifier webProcessIdentifier)
{
    RELEASE_LOG_ERROR(Process, "GPUProcessProxy::terminateWebProcess: webProcessIdentifier=%" PRIu64, webProcessIdentifier.toUInt64());
    if (auto process = WebProcessProxy::processForIdentifier(webProcessIdentifier))
        process->requestTermination(ProcessTerminationReason::RequestedByGPUProcess);
}

#if HAVE(VISIBILITY_PROPAGATION_VIEW)
void GPUProcessProxy::didCreateContextForVisibilityPropagation(WebPageProxyIdentifier webPageProxyID, WebCore::PageIdentifier pageID, LayerHostingContextID contextID)
{
    RELEASE_LOG(Process, "%p - GPUProcessProxy::didCreateContextForVisibilityPropagation: webPageProxyID: %" PRIu64 ", pagePID: %" PRIu64 ", contextID: %u", this, webPageProxyID.toUInt64(), pageID.toUInt64(), contextID);
    RefPtr page = WebProcessProxy::webPage(webPageProxyID);
    if (!page) {
        RELEASE_LOG(Process, "%p - GPUProcessProxy::didCreateContextForVisibilityPropagation() No WebPageProxy with this identifier", this);
        return;
    }
    if (page->webPageIDInMainFrameProcess() == pageID) {
        page->didCreateContextInGPUProcessForVisibilityPropagation(contextID);
        return;
    }
    RefPtr provisionalPage = page->provisionalPageProxy();
    if (provisionalPage && provisionalPage->webPageID() == pageID) {
        provisionalPage->didCreateContextInGPUProcessForVisibilityPropagation(contextID);
        return;
    }
    RELEASE_LOG(Process, "%p - GPUProcessProxy::didCreateContextForVisibilityPropagation() There was a WebPageProxy for this identifier, but it had the wrong WebPage identifier.", this);
}
#endif

#if PLATFORM(MAC)
void GPUProcessProxy::setScreenProperties(const WebCore::ScreenProperties& properties)
{
    send(Messages::GPUProcess::SetScreenProperties { properties }, 0);
}
#endif

void GPUProcessProxy::updatePreferences(WebProcessProxy& webProcess)
{
    if (!canSendMessage())
        return;

    // FIXME (https://bugs.webkit.org/show_bug.cgi?id=241821): It's not ideal that these features are controlled by preferences-level feature flags (i.e. per-web view), but there is only
    // one GPU process and the runtime-enabled features backing these preferences are process-wide. We should refactor each of these features
    // so that they aren't process-global, and then reimplement this feature flag propagation to the GPU Process in a way that respects the
    // settings of the page that is hosting each media element.
    // For the time being, each of the below features are enabled in the GPU Process if it is enabled by at least one web page's preferences.
    // In practice, all web pages' preferences should agree on these feature flag values.
    GPUProcessPreferences gpuPreferences;
    for (Ref page : webProcess.pages()) {
        Ref webPreferences = page->preferences();
        if (!webPreferences->useGPUProcessForMediaEnabled())
            continue;
        gpuPreferences.copyEnabledWebPreferences(webPreferences);
    }
    
    send(Messages::GPUProcess::UpdateGPUProcessPreferences(gpuPreferences), 0);
}

void GPUProcessProxy::updateScreenPropertiesIfNeeded()
{
#if PLATFORM(MAC)
    if (!canSendMessage())
        return;

    setScreenProperties(collectScreenProperties());
#endif
}

void GPUProcessProxy::didBecomeUnresponsive()
{
    RELEASE_LOG_ERROR(Process, "%p - GPUProcessProxy::didBecomeUnresponsive: GPUProcess with PID %d became unresponsive, terminating it", this, processID());
    terminate();
    gpuProcessExited(ProcessTerminationReason::Unresponsive);
}

#if !PLATFORM(COCOA)
void GPUProcessProxy::platformInitializeGPUProcessParameters(GPUProcessCreationParameters& parameters)
{
}
#endif


#if HAVE(AUDIT_TOKEN)
void GPUProcessProxy::setPresentingApplicationAuditToken(WebCore::ProcessIdentifier processIdentifier, WebCore::PageIdentifier pageIdentifier, std::optional<CoreIPCAuditToken> auditToken)
{
    send(Messages::GPUProcess::SetPresentingApplicationAuditToken(processIdentifier, pageIdentifier, auditToken), 0);
}
#endif

#if PLATFORM(VISION) && ENABLE(MODEL_PROCESS)
#if HAVE(CORE_RE)
void GPUProcessProxy::requestSharedSimulationConnection(audit_token_t modelProcessAuditToken, CompletionHandler<void(std::optional<IPC::SharedFileHandle>)>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::RequestSharedSimulationConnection { modelProcessAuditToken }, WTF::move(completionHandler));
}
#endif

#if HAVE(TASK_IDENTITY_TOKEN)
void GPUProcessProxy::createMemoryAttributionIDForTask(WebCore::ProcessIdentity processIdentity, CompletionHandler<void(const std::optional<String>&)>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::CreateMemoryAttributionIDForTask { WTF::move(processIdentity) }, WTF::move(completionHandler));
}

void GPUProcessProxy::unregisterMemoryAttributionID(const String& attributionID, CompletionHandler<void()>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::UnregisterMemoryAttributionID { attributionID }, WTF::move(completionHandler));
}
#endif
#endif

void GPUProcessProxy::sinkCompletedSnapshotToBitmap(RemoteSnapshotIdentifier identifier, const WebCore::FloatSize& size, WebCore::FrameIdentifier rootFrameIdentifier, CompletionHandler<void(std::optional<WebCore::ShareableBitmap::Handle>&&)>&& completionHandler)
{
    sendWithAsyncReply(Messages::GPUProcess::SinkCompletedSnapshotToBitmap(identifier, size, rootFrameIdentifier), WTF::move(completionHandler));
}

void GPUProcessProxy::releaseSnapshot(RemoteSnapshotIdentifier identifier)
{
    send(Messages::GPUProcess::ReleaseSnapshot(identifier), 0);
}

} // namespace WebKit

#endif // ENABLE(GPU_PROCESS)
