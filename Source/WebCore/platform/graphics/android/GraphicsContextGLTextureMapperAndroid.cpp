/*
 * Copyright (C) 2025 Igalia S.L.
 *
 * This library is free software; you can redistribute it and/or
 * modify it under the terms of the GNU Library General Public
 * License as published by the Free Software Foundation; either
 * version 2 of the License, or (at your option) any later version.
 *
 * This library is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
 * Library General Public License for more details.
 *
 * You should have received a copy of the GNU Library General Public License
 * along with this library; see the file COPYING.LIB.  If not, write to
 * the Free Software Foundation, Inc., 51 Franklin Street, Fifth Floor,
 * Boston, MA 02110-1301, USA.
 */

#include "config.h"
#include "GraphicsContextGLTextureMapperAndroid.h"

#if ENABLE(WEBGL) && USE(COORDINATED_GRAPHICS) && OS(ANDROID)
#include "ANGLEHeaders.h"
#include "GLFence.h"
#include "Logging.h"
#include "PlatformDisplay.h"
#include "TextureMapperFlags.h"
#include <android/hardware_buffer.h>

namespace WebCore {

RefPtr<GraphicsContextGLTextureMapperAndroid> GraphicsContextGLTextureMapperAndroid::create(GraphicsContextGLAttributes&& attributes)
{
    auto context = adoptRef(new GraphicsContextGLTextureMapperAndroid(WTF::move(attributes)));
    if (!context->initialize())
        return nullptr;
    return context;
}

bool GraphicsContextGLTextureMapperAndroid::platformInitializeExtensions()
{
    if (!enableExtensionsImpl({ "GL_OES_EGL_image"_s }))
        return false;


    const auto& eglExtensions = PlatformDisplay::sharedDisplay().eglExtensions();
    return eglExtensions.KHR_image_base && eglExtensions.ANDROID_get_native_client_buffer && eglExtensions.ANDROID_image_native_buffer;
}


} // namespace WebCore

#endif // ENABLE(WEBGL) && USE(COORDINATED_GRAPHICS) && OS(ANDROID)
