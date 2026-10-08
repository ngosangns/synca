<?php

namespace App\Http\Controllers;

use App\Models\CommandLog;
use Illuminate\Http\Request;
use Illuminate\View\View;

class LogController extends Controller
{
    private const PAGE = 30;

    /**
     * Partial log feed for the log pane.
     *   ?before=<id> → older entries (lazy load on scroll)
     *   ?after=<id>  → newer entries (polling)
     */
    public function feed(Request $request): View
    {
        $query = CommandLog::query();
        if ($before = $request->integer('before')) {
            $entries = $query->where('id', '<', $before)->latest('id')->limit(self::PAGE)->get();
        } elseif ($after = $request->integer('after')) {
            $entries = $query->where('id', '>', $after)->latest('id')->get();
        } else {
            $entries = $query->latest('id')->limit(self::PAGE)->get();
        }

        return view('dashboard._logs', ['logs' => $entries]);
    }
}
