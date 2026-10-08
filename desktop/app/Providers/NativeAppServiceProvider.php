<?php

namespace App\Providers;

use Native\Desktop\Contracts\ProvidesPhpIni;
use Native\Desktop\Facades\Menu;
use Native\Desktop\Facades\Window;

class NativeAppServiceProvider implements ProvidesPhpIni
{
    public function boot(): void
    {
        Menu::create(
            Menu::app(),
            Menu::make(
                Menu::route('dashboard', 'Dashboard', 'CmdOrCtrl+1'),
                Menu::separator(),
                Menu::close(),
            )->label('File'),
            Menu::edit(),
            Menu::view(),
            Menu::window(),
        );

        Window::open()
            ->title(config('app.name'))
            ->width(1280)
            ->height(800)
            ->minWidth(640)
            ->minHeight(480)
            ->titleBarHiddenInset()
            ->trafficLightPosition(16, 8)
            ->rememberState();
    }

    public function phpIni(): array
    {
        return [
            'memory_limit' => '256M',
            'opcache.jit' => 'off',
        ];
    }
}
