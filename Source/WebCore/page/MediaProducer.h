/*
 * Copyright (C) 2014-2026 Apple Inc. All rights reserved.
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
 * THIS SOFTWARE IS PROVIDED BY APPLE INC. ``AS IS'' AND ANY
 * EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
 * PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL APPLE INC. OR
 * CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
 * EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
 * PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
 * PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY
 * OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

#pragma once

#include <wtf/AbstractRefCountedAndCanMakeWeakPtr.h>
#include <wtf/OptionSet.h>
#include <wtf/Platform.h>
#include <wtf/WeakPtr.h>

namespace WebCore {

enum class MediaProducerMediaState : uint32_t {
    IsPlayingAudio = 1 << 0,
    IsPlayingVideo = 1 << 1,
    IsPlayingToExternalDevice = 1 << 2,
    RequiresPlaybackTargetMonitoring = 1 << 3,
    ExternalDeviceAutoPlayCandidate = 1 << 4,
    DidPlayToEnd = 1 << 5,
    IsSourceElementPlaying = 1 << 6,
    IsNextTrackControlEnabled = 1 << 7,
    IsPreviousTrackControlEnabled = 1 << 8,
    HasPlaybackTargetAvailabilityListener = 1 << 9,
    HasAudioOrVideo = 1 << 10,
    HasUserInteractedWithMediaElement = 1 << 17,
    HasStreamingActivity = 1 << 27,
};
using MediaProducerMediaStateFlags = OptionSet<MediaProducerMediaState>;


enum class MediaProducerMutedState : uint8_t {
    AudioIsMuted = 1 << 0,
};
using MediaProducerMutedStateFlags = OptionSet<MediaProducerMutedState>;

class MediaProducer : public AbstractRefCountedAndCanMakeWeakPtr<MediaProducer> {
public:
    using MediaState = MediaProducerMediaState;
    using MutedState = MediaProducerMutedState;
    using MediaStateFlags = MediaProducerMediaStateFlags;
    using MutedStateFlags = MediaProducerMutedStateFlags;

    static constexpr MediaStateFlags IsNotPlaying = { };

#if ENABLE(EXTENSION_CAPABILITIES)
    static bool needsMediaCapability(MediaStateFlags state)
    {
        if (state.contains(MediaProducerMediaState::IsPlayingAudio))
            return true;

        return state.contains(MediaProducerMediaState::IsPlayingVideo);
    }
#endif

    virtual MediaStateFlags mediaState() const = 0;


    virtual void visibilityAdjustmentStateDidChange() { }
    virtual void pageMutedStateDidChange() = 0;

#if PLATFORM(IOS_FAMILY)
    virtual void sceneIdentifierDidChange() { }
#endif

protected:
    virtual ~MediaProducer() = default;
};

} // namespace WebCore
